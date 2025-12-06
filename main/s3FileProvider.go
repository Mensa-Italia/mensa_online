package main

import (
	"context"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"strings"
)

// Modified from Pocketbase's codebase
func NewS3(
	bucketName string,
	region string,
	endpoint string,
	accessKey string,
	secretKey string,
	s3ForcePathStyle bool,
) (*s3.Client, error) {
	ctx := context.Background() // default context

	cred := credentials.NewStaticCredentialsProvider(accessKey, secretKey, "")

	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithCredentialsProvider(cred),
		config.WithRegion(region),
		config.WithEndpointResolverWithOptions(aws.EndpointResolverWithOptionsFunc(func(service, region string, options ...interface{}) (aws.Endpoint, error) {
			// ensure that the endpoint has url scheme for
			// backward compatibility with v1 of the aws sdk
			prefixedEndpoint := endpoint
			if !strings.Contains(endpoint, "://") {
				prefixedEndpoint = "https://" + endpoint
			}

			return aws.Endpoint{URL: prefixedEndpoint, SigningRegion: region}, nil
		})),
	)
	if err != nil {
		return nil, err

	}

	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.UsePathStyle = s3ForcePathStyle
	})

	return client, nil
}
