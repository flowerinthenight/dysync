package main

import (
	"fmt"
	"log"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/credentials/stscreds"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/dynamodb"
	"github.com/flowerinthenight/libdy"
	"github.com/spf13/cobra"
)

var (
	srcRegion  string
	srcKey     string
	srcSecret  string
	srcRoleArn string
	dstRegion  string
	dstKey     string
	dstSecret  string
	dstRoleArn string
	id         string
	sk         string
	concurrent int
	copyOnly   bool
	dryrun     bool

	rootCmd = &cobra.Command{
		Use:   "dysync <table>",
		Short: "sync DynamoDB table across two accounts",
		Long: `Sync DynamoDB table across two AWS accounts.

To authenticate to AWS, you can set the following environment variables:
  [required]
  AWS_REGION
  AWS_ACCESS_KEY_ID
  AWS_SECRET_ACCESS_KEY

  [optional]
  ROLE_ARN

When role ARN is provided, this tool will attempt to assume that role using the
provided key/secret combination.

By default, this tool will attempt to do a full sync, meaning, any records in
dst that don't exist in src will be deleted. To override, set --copy-only to
true, in which case, it will only do a copy. This option only works when both
--id and --sk are empty (scan table), otherwise, if either --id or --sk is set
(or both), it will do a copy only, not full sync.

Note that this cmd uses the Scan AWS API which is expensive and really slow for
huge tables. You've been warned.`,
		RunE: run,
	}
)

func run(cmd *cobra.Command, args []string) error {
	log.SetFlags(0)
	log.SetOutput(os.Stdout)

	defer func(begin time.Time) {
		log.Println("duration:", time.Since(begin))
	}(time.Now())

	if len(args) == 0 {
		return fmt.Errorf("table cannot be empty")
	}

	table := args[0]
	if srcKey == dstKey && srcSecret == dstSecret {
		return fmt.Errorf("cannot use the same credentials")
	}

	var srcdy, dstdy *dynamodb.DynamoDB
	srcSess, _ := session.NewSession(&aws.Config{
		Region:      aws.String(srcRegion),
		Credentials: credentials.NewStaticCredentials(srcKey, srcSecret, ""),
	})

	if srcRoleArn != "" {
		srcCnf := &aws.Config{Credentials: stscreds.NewCredentials(srcSess, srcRoleArn)}
		srcdy = dynamodb.New(srcSess, srcCnf)
	} else {
		srcdy = dynamodb.New(srcSess)
	}

	dstSess, _ := session.NewSession(&aws.Config{
		Region:      aws.String(dstRegion),
		Credentials: credentials.NewStaticCredentials(dstKey, dstSecret, ""),
	})

	if dstRoleArn != "" {
		dstCnf := &aws.Config{Credentials: stscreds.NewCredentials(dstSess, dstRoleArn)}
		dstdy = dynamodb.New(srcSess, dstCnf)
	} else {
		dstdy = dynamodb.New(dstSess)
	}

	var tHash, tRange string
	t, err := srcdy.DescribeTable(&dynamodb.DescribeTableInput{TableName: aws.String(table)})
	if err != nil {
		return err
	}

	for _, v := range t.Table.KeySchema {
		if *v.KeyType == "HASH" {
			tHash = *v.AttributeName
		}

		if *v.KeyType == "RANGE" {
			tRange = *v.AttributeName
		}
	}

	var items []map[string]*dynamodb.AttributeValue
	if id == "" && sk == "" {
		items, err = libdy.ScanItems(srcdy, table)
		if err != nil {
			return fmt.Errorf("ScanItems failed: %w", err)
		}
	} else {
		var err error
		items, err = libdy.GetItems(srcdy, table, id, sk)
		if err != nil {
			return fmt.Errorf("GetItems failed: %w", err)
		}
	}

	srcm := make(map[string]struct{})
	var cpcnt, cpf, delcnt, delf int
	num := len(items)
	putjobs := make(chan map[string]*dynamodb.AttributeValue, num)
	putres := make(chan error, num)

	for i := 0; i < concurrent; i++ {
		go func(id int, jobs <-chan map[string]*dynamodb.AttributeValue, res chan<- error) {
			var pfx string
			for j := range jobs {
				if !dryrun {
					res <- libdy.PutItem(dstdy, table, j)
				} else {
					res <- nil
					pfx = "[dryrun] "
				}

				if tRange != "" {
					log.Printf("%vsync: %v, %v", pfx, *j[tHash].S, *j[tRange].S)
				} else {
					log.Printf("%vsync: %+v", pfx, *j[tHash].S)
				}
			}
		}(i, putjobs, putres)
	}

	for _, item := range items {
		putjobs <- item
		if tRange != "" {
			k := fmt.Sprintf("%v*****%v", *item[tHash].S, *item[tRange].S)
			srcm[k] = struct{}{}
		} else {
			k := fmt.Sprintf("%v", *item[tHash].S)
			srcm[k] = struct{}{}
		}
	}

	close(putjobs)
	for i := 0; i < num; i++ {
		err := <-putres
		if err != nil {
			log.Println("PutItem failed:", err)
			cpf++
		} else {
			cpcnt++
		}
	}

	// Implied, for now.
	if id != "" || sk != "" {
		copyOnly = true
	}

	if !copyOnly {
		items, err = libdy.ScanItems(dstdy, table)
		if err != nil {
			return fmt.Errorf("ScanItems failed: %w", err)
		}

		type ret struct {
			err error
			del bool
		}

		num := len(items)
		deljobs := make(chan map[string]*dynamodb.AttributeValue, num)
		delres := make(chan ret, num)

		for i := 0; i < concurrent; i++ {
			go func(id int, jobs <-chan map[string]*dynamodb.AttributeValue, res chan<- ret) {
				for j := range jobs {
					var k string
					if tRange != "" {
						k = fmt.Sprintf("%v*****%v", *j[tHash].S, *j[tRange].S)
					} else {
						k = fmt.Sprintf("%v", *j[tHash].S)
					}

					var err error
					var del bool
					if _, ok := srcm[k]; !ok {
						var pfx string
						kk := strings.Split(k, "*****")
						if !dryrun {
							if len(kk) > 1 {
								pk := fmt.Sprintf("%v:%v", tHash, kk[0])
								sk := fmt.Sprintf("%v:%v", tRange, kk[1])
								err, del = libdy.DeleteItem(dstdy, table, pk, sk), true
							} else {
								pk := fmt.Sprintf("%v:%v", tHash, kk[0])
								err, del = libdy.DeleteItem(dstdy, table, pk, ""), true
							}
						} else {
							pfx = "[dryrun] "
						}

						if len(kk) > 1 {
							log.Printf("%vdelete: %v, %v", pfx, kk[0], kk[1])
						} else {
							log.Printf("%vdelete: %v", pfx, kk[0])
						}
					}

					res <- ret{err, del}
				}
			}(i, deljobs, delres)
		}

		for _, item := range items {
			deljobs <- item
		}

		close(deljobs)
		for i := 0; i < num; i++ {
			r := <-delres
			if r.err != nil {
				log.Println("DeleteItem failed:", err)
				delf++
			} else {
				if r.del {
					delcnt++
				}
			}
		}
	}

	log.Printf("syncd=%v, failed=%v, deleted=%v, failed=%v", cpcnt, cpf, delcnt, delf)
	return nil

}

func main() {
	rootCmd.Flags().SortFlags = false
	rootCmd.Flags().StringVar(&srcRegion, "src-region", os.Getenv("AWS_REGION"), "source acct region")
	rootCmd.Flags().StringVar(&srcKey, "src-key", os.Getenv("AWS_ACCESS_KEY_ID"), "source acct access key")
	rootCmd.Flags().StringVar(&srcSecret, "src-secret", os.Getenv("AWS_SECRET_ACCESS_KEY"), "source acct secret key")
	rootCmd.Flags().StringVar(&srcRoleArn, "src-rolearn", os.Getenv("ROLE_ARN"), "optional source role arn")
	rootCmd.Flags().StringVar(&dstRegion, "dst-region", os.Getenv("AWS_REGION"), "target acct region")
	rootCmd.Flags().StringVar(&dstKey, "dst-key", dstKey, "target acct access key")
	rootCmd.Flags().StringVar(&dstSecret, "dst-secret", dstSecret, "target acct secret key")
	rootCmd.Flags().StringVar(&dstRoleArn, "dst-rolearn", dstRoleArn, "optional target rolearn")
	rootCmd.Flags().StringVar(&id, "id", id, "optional filter: source 'id' to sync (only string is supported for now)")
	rootCmd.Flags().StringVar(&sk, "sk", sk, "optional filter: source 'sort_key' to sync (only string is supported for now)")
	rootCmd.Flags().BoolVar(&copyOnly, "copy-only", copyOnly, "if true, copy src to dst only, no full sync, default to true when id|sk is not empty")
	rootCmd.Flags().IntVar(&concurrent, "concurrent", runtime.NumCPU()*2, "number of worker goroutines to handle the copy and delete")
	rootCmd.Flags().BoolVar(&dryrun, "dryrun", dryrun, "dryrun, no write")
	rootCmd.Execute()
}
