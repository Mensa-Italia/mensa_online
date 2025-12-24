package cdnfiles

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
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

func UploadFileToS3(app core.App, bucket, fileKey string, file []byte, metadata ...map[string]string) error {
	s3settings := app.Settings().S3
	if !s3settings.Enabled {
		return errors.New("s3 is disabled")
	}

	s3client, err := NewS3(s3settings.Region, s3settings.Endpoint, s3settings.AccessKey, s3settings.Secret, s3settings.ForcePathStyle)
	if err != nil {
		return err
	}

	if len(metadata) == 0 {
		metadata = append(metadata, map[string]string{})
	}

	_, err = s3client.PutObject(context.Background(), &s3.PutObjectInput{
		Bucket:   aws.String(bucket),
		Key:      aws.String(fileKey),
		Body:     bytes.NewReader(file),
		Metadata: metadata[0],
	})
	if err != nil {
		return err
	}

	return nil
}

func RetrieveFileFromS3(app core.App, bucket, fileKey string) ([]byte, map[string]string, error) {
	s3settings := app.Settings().S3
	if !s3settings.Enabled {
		return nil, nil, errors.New("s3 is disabled")
	}

	s3client, err := NewS3(s3settings.Region, s3settings.Endpoint, s3settings.AccessKey, s3settings.Secret, s3settings.ForcePathStyle)
	if err != nil {
		return nil, nil, err
	}

	out, err := s3client.GetObject(context.Background(), &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(fileKey),
	})

	if err != nil {
		return nil, nil, err
	}
	defer func() {
		_ = out.Body.Close()
	}()

	b, err := io.ReadAll(out.Body)
	if err != nil {
		return nil, nil, err
	}

	return b, out.Metadata, nil
}

// ListKeysInS3Prefix ritorna una mappa dove:
//   - key:      la key completa dell'oggetto S3 (es. "snapshots/a.json")
//   - value:    metadata dell'oggetto (out.Metadata)
//
// Nota: la Metadata non viene restituita da ListObjectsV2, quindi viene recuperata con una chiamata HeadObject per ogni key.
//
// Esempi:
//
//	prefix = "snapshots/"  -> include "snapshots/a.json", "snapshots/sub/b.json", ...
//	prefix = "snapshots"   -> viene normalizzato a "snapshots/"
func ListKeysInS3Prefix(app core.App, bucket, prefix string) (map[string]map[string]string, error) {
	s3settings := app.Settings().S3
	if !s3settings.Enabled {
		return nil, errors.New("s3 is disabled")
	}

	// Normalizza il prefix per comportarsi come una "cartella".
	prefix = strings.TrimSpace(prefix)
	if prefix != "" && !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}

	s3client, err := NewS3(s3settings.Region, s3settings.Endpoint, s3settings.AccessKey, s3settings.Secret, s3settings.ForcePathStyle)
	if err != nil {
		return nil, err
	}

	p := s3.NewListObjectsV2Paginator(s3client, &s3.ListObjectsV2Input{
		Bucket: aws.String(bucket),
		Prefix: aws.String(prefix),
	})

	ctx := context.Background()
	result := make(map[string]map[string]string, 128)
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return nil, err
		}

		for _, obj := range page.Contents {
			if obj.Key == nil || strings.TrimSpace(*obj.Key) == "" {
				continue
			}

			key := *obj.Key
			head, err := s3client.HeadObject(ctx, &s3.HeadObjectInput{
				Bucket: aws.String(bucket),
				Key:    aws.String(key),
			})
			if err != nil {
				return nil, err
			}

			result[key] = head.Metadata
		}
	}

	return result, nil
}
