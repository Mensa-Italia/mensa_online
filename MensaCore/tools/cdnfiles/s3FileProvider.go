package cdnfiles

import (
	"context"
	"strings"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// Modified from Pocketbase's codebase
func NewS3(
	region string,
	endpoint string,
	accessKey string,
	secretKey string,
	s3ForcePathStyle bool,
) (*s3.Client, error) {
	ctx := context.Background() // default context

	cred := credentials.NewStaticCredentialsProvider(accessKey, secretKey, "")

	// ensure that the endpoint has url scheme for
	// backward compatibility with v1 of the aws sdk
	prefixedEndpoint := endpoint
	if !strings.Contains(endpoint, "://") {
		prefixedEndpoint = "https://" + endpoint
	}

	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithCredentialsProvider(cred),
		config.WithRegion(region),
		config.WithBaseEndpoint(prefixedEndpoint),
	)
	if err != nil {
		return nil, err
	}

	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.UsePathStyle = s3ForcePathStyle
	})

	return client, nil
}
