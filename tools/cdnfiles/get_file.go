package cdnfiles

import (
	"bytes"
	"context"
	"errors"
	"io"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/pocketbase/pocketbase/core"
)

func GetFilePresignedURL(app core.App, bucket, fileKey string) string {
	s3settings := app.Settings().S3
	if s3settings.Enabled {
		s3client, err := NewS3(s3settings.Region, s3settings.Endpoint, s3settings.AccessKey, s3settings.Secret, s3settings.ForcePathStyle)
		if err != nil {
			app.Logger().Error("create s3 client", err)
			return ""
		}
		presignClient := s3.NewPresignClient(s3client)
		presignedUrl, err := presignClient.PresignGetObject(context.Background(),
			&s3.GetObjectInput{
				Bucket: aws.String(bucket),
				Key:    aws.String(fileKey),
			},
			s3.WithPresignExpires(time.Hour))
		if err != nil {
			app.Logger().Error("create s3 presigned url", err)
			return ""
		}
		return presignedUrl.URL
	}

	return ""
}

func UploadFileToS3(app core.App, bucket, fileKey string, file []byte) error {
	s3settings := app.Settings().S3
	if !s3settings.Enabled {
		return errors.New("s3 is disabled")
	}

	s3client, err := NewS3(s3settings.Region, s3settings.Endpoint, s3settings.AccessKey, s3settings.Secret, s3settings.ForcePathStyle)
	if err != nil {
		return err
	}

	_, err = s3client.PutObject(context.Background(), &s3.PutObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(fileKey),
		Body:   bytes.NewReader(file),
	})
	if err != nil {
		return err
	}

	return nil
}

func RetrieveFileFromS3(app core.App, bucket, fileKey string) ([]byte, error) {
	s3settings := app.Settings().S3
	if !s3settings.Enabled {
		return nil, errors.New("s3 is disabled")
	}

	s3client, err := NewS3(s3settings.Region, s3settings.Endpoint, s3settings.AccessKey, s3settings.Secret, s3settings.ForcePathStyle)
	if err != nil {
		return nil, err
	}

	out, err := s3client.GetObject(context.Background(), &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(fileKey),
	})
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = out.Body.Close()
	}()

	b, err := io.ReadAll(out.Body)
	if err != nil {
		return nil, err
	}

	return b, nil
}
