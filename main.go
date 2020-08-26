package main

import (
	"fmt"
	"log"
	"os"
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
	table      string
	id         string
	sk         string
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

	if table == "" {
		return fmt.Errorf("table cannot be empty")
	}

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

	var prfx string
	srcm := make(map[string]struct{})
	var count, failed int
	for _, item := range items {
		if tRange != "" {
			k := fmt.Sprintf("%v*****%v", *item[tHash].S, *item[tRange].S)
			srcm[k] = struct{}{}
		} else {
			k := fmt.Sprintf("%v", *item[tHash].S)
			srcm[k] = struct{}{}
		}

		if !dryrun {
			err := libdy.PutItem(dstdy, table, item)
			if err != nil {
				log.Println("PutItem failed:", err)
				failed += 1
				continue
			}
		} else {
			prfx = "[dryrun] "
		}

		if tRange != "" {
			log.Printf("%vsync: %v, %v", prfx, *item[tHash].S, *item[tRange].S)
		} else {
			log.Printf("%vsync: %+v", prfx, *item[tHash].S)
		}

		count += 1
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

		for _, item := range items {
			var k string
			if tRange != "" {
				k = fmt.Sprintf("%v*****%v", *item[tHash].S, *item[tRange].S)
			} else {
				k = fmt.Sprintf("%v", *item[tHash].S)
			}

			if _, ok := srcm[k]; !ok {
				kk := strings.Split(k, "*****")
				if !dryrun {
					if len(kk) > 1 {
						err = libdy.DeleteItem(dstdy, table, kk[0], kk[1])
					} else {
						err = libdy.DeleteItem(dstdy, table, tHash, "")
					}

					if err != nil {
						log.Printf("DeleteItem failed: %v", err)
						continue
					}
				}

				if len(kk) > 1 {
					log.Printf("%vdelete: %v, %v", prfx, kk[0], kk[1])
				} else {
					log.Printf("%vdelete: %v", prfx, kk[0])
				}
			}
		}
	}

	log.Printf("syncd=%v, failed=%v", count, failed)
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
	rootCmd.Flags().StringVar(&table, "table", table, "dynamodb table to sync")
	rootCmd.Flags().StringVar(&id, "id", id, "optional filter: source 'id' to sync (only string is supported for now)")
	rootCmd.Flags().StringVar(&sk, "sk", sk, "optional filter: source 'sort_key' to sync (only string is supported for now)")
	rootCmd.Flags().BoolVar(&copyOnly, "copy-only", copyOnly, "if true, copy src to dst only, no full sync, default to true when id|sk is not empty")
	rootCmd.Flags().BoolVar(&dryrun, "dryrun", dryrun, "dryrun, no write")
	rootCmd.Execute()
}
