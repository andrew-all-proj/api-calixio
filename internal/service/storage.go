package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"time"

	"calixio/internal/config"
	"calixio/internal/repository"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
)

type StorageService struct {
	s3Client  *s3.Client
	presigner *s3.PresignClient
	bucket    string
	region    string
	endpoint  string
	pathStyle bool
	publicURL string
}

func NewStorageService(ctx context.Context, cfg *config.Config) (*StorageService, error) {
	var awsCfg aws.Config
	var err error
	if cfg.AWS.AccessKeyID != "" && cfg.AWS.SecretAccessKey != "" {
		awsCfg, err = awsconfig.LoadDefaultConfig(ctx,
			awsconfig.WithRegion(cfg.AWS.Region),
			awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(cfg.AWS.AccessKeyID, cfg.AWS.SecretAccessKey, "")),
		)
	} else {
		awsCfg, err = awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(cfg.AWS.Region))
	}
	if err != nil {
		return nil, err
	}

	clientOpts := []func(*s3.Options){
		func(o *s3.Options) {
			o.Region = cfg.AWS.Region
		},
	}

	if cfg.AWS.Endpoint != "" {
		clientOpts = append(clientOpts, func(o *s3.Options) {
			o.EndpointResolver = s3.EndpointResolverFromURL(cfg.AWS.Endpoint)
		})
	}

	if cfg.AWS.PathStyle {
		clientOpts = append(clientOpts, func(o *s3.Options) {
			o.UsePathStyle = true
		})
	}

	s3Client := s3.NewFromConfig(awsCfg, clientOpts...)

	return &StorageService{
		s3Client:  s3Client,
		presigner: s3.NewPresignClient(s3Client),
		bucket:    cfg.AWS.Bucket,
		region:    cfg.AWS.Region,
		endpoint:  cfg.AWS.Endpoint,
		pathStyle: cfg.AWS.PathStyle,
		publicURL: cfg.AWS.PublicURL,
	}, nil
}

func (s *StorageService) PresignPutObject(ctx context.Context, key, contentType string, expires time.Duration) (string, error) {
	if s.bucket == "" {
		return "", fmt.Errorf("s3 bucket is not configured")
	}
	if expires <= 0 {
		expires = 15 * time.Minute
	}

	input := &s3.PutObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	}
	if contentType != "" {
		input.ContentType = aws.String(contentType)
	}

	resp, err := s.presigner.PresignPutObject(ctx, input, s3.WithPresignExpires(expires))
	if err != nil {
		return "", err
	}
	return resp.URL, nil
}

func (s *StorageService) PresignGetObject(ctx context.Context, key string, expires time.Duration) (string, error) {
	if s.bucket == "" {
		return "", fmt.Errorf("s3 bucket is not configured")
	}
	if expires <= 0 {
		expires = 15 * time.Minute
	}

	resp, err := s.presigner.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	}, s3.WithPresignExpires(expires))
	if err != nil {
		return "", err
	}
	return resp.URL, nil
}

type ObjectHead struct {
	ContentLength int64
	ContentType   string
	ETag          string
	LastModified  time.Time
}

func (s *StorageService) HeadObject(ctx context.Context, key string) (ObjectHead, error) {
	if s.bucket == "" {
		return ObjectHead{}, fmt.Errorf("s3 bucket is not configured")
	}

	out, err := s.s3Client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		var apiErr smithy.APIError
		if errors.As(err, &apiErr) {
			code := apiErr.ErrorCode()
			if code == "NotFound" || code == "NoSuchKey" {
				return ObjectHead{}, repository.ErrNotFound
			}
		}
		return ObjectHead{}, err
	}

	head := ObjectHead{
		ContentLength: aws.ToInt64(out.ContentLength),
		ETag:          aws.ToString(out.ETag),
		LastModified:  aws.ToTime(out.LastModified),
	}
	if out.ContentType != nil {
		head.ContentType = *out.ContentType
	}
	return head, nil
}

func (s *StorageService) DownloadObjectToFile(ctx context.Context, key, filePath string) error {
	if s.bucket == "" {
		return fmt.Errorf("s3 bucket is not configured")
	}

	out, err := s.s3Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return err
	}
	defer out.Body.Close()

	f, err := os.Create(filePath)
	if err != nil {
		return err
	}
	defer f.Close()

	if _, err := io.Copy(f, out.Body); err != nil {
		return err
	}
	return nil
}

func (s *StorageService) GetObjectBytes(ctx context.Context, key string) ([]byte, error) {
	if s.bucket == "" {
		return nil, fmt.Errorf("s3 bucket is not configured")
	}

	out, err := s.s3Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, err
	}
	defer out.Body.Close()

	data, err := io.ReadAll(out.Body)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func (s *StorageService) UploadFile(ctx context.Context, key, contentType, filePath string) error {
	return s.uploadFile(ctx, key, contentType, filePath, "")
}

func (s *StorageService) UploadFilePublic(ctx context.Context, key, contentType, filePath string) error {
	return s.uploadFile(ctx, key, contentType, filePath, "public-read")
}

func (s *StorageService) DeleteObject(ctx context.Context, key string) error {
	if s.bucket == "" {
		return fmt.Errorf("s3 bucket is not configured")
	}

	_, err := s.s3Client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	return err
}

func (s *StorageService) DeleteObjectsByPrefix(ctx context.Context, prefix string) error {
	if s.bucket == "" {
		return fmt.Errorf("s3 bucket is not configured")
	}
	trimmedPrefix := strings.TrimSpace(prefix)
	if trimmedPrefix == "" {
		return errors.New("prefix is required")
	}

	pager := s3.NewListObjectsV2Paginator(s.s3Client, &s3.ListObjectsV2Input{
		Bucket: aws.String(s.bucket),
		Prefix: aws.String(trimmedPrefix),
	})

	toDelete := make([]s3types.ObjectIdentifier, 0, 1000)
	flush := func() error {
		if len(toDelete) == 0 {
			return nil
		}
		_, err := s.s3Client.DeleteObjects(ctx, &s3.DeleteObjectsInput{
			Bucket: aws.String(s.bucket),
			Delete: &s3types.Delete{Objects: toDelete, Quiet: aws.Bool(true)},
		})
		if err != nil {
			return err
		}
		toDelete = toDelete[:0]
		return nil
	}

	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return err
		}
		for _, obj := range page.Contents {
			key := strings.TrimSpace(aws.ToString(obj.Key))
			if key == "" {
				continue
			}
			toDelete = append(toDelete, s3types.ObjectIdentifier{Key: aws.String(key)})
			if len(toDelete) == 1000 {
				if err := flush(); err != nil {
					return err
				}
			}
		}
	}

	return flush()
}

func (s *StorageService) ListMediaPrefixes(ctx context.Context) (map[string]string, error) {
	if s.bucket == "" {
		return nil, fmt.Errorf("s3 bucket is not configured")
	}

	pager := s3.NewListObjectsV2Paginator(s.s3Client, &s3.ListObjectsV2Input{
		Bucket: aws.String(s.bucket),
		Prefix: aws.String("users/"),
	})

	prefixes := make(map[string]string)
	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, obj := range page.Contents {
			key := strings.TrimSpace(aws.ToString(obj.Key))
			mediaID, mediaPrefix, ok := extractMediaPrefix(key)
			if !ok {
				continue
			}
			if _, exists := prefixes[mediaID]; !exists {
				prefixes[mediaID] = mediaPrefix
			}
		}
	}

	return prefixes, nil
}

func (s *StorageService) uploadFile(ctx context.Context, key, contentType, filePath, acl string) error {
	if s.bucket == "" {
		return fmt.Errorf("s3 bucket is not configured")
	}

	f, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer f.Close()

	input := &s3.PutObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
		Body:   f,
	}
	if contentType != "" {
		input.ContentType = aws.String(contentType)
	}
	if strings.TrimSpace(acl) != "" {
		input.ACL = s3types.ObjectCannedACL(strings.TrimSpace(acl))
	}

	_, err = s.s3Client.PutObject(ctx, input)
	return err
}

func extractMediaPrefix(key string) (mediaID string, prefix string, ok bool) {
	parts := strings.Split(strings.Trim(strings.TrimSpace(key), "/"), "/")
	if len(parts) < 4 {
		return "", "", false
	}
	if parts[0] != "users" || parts[2] != "media" {
		return "", "", false
	}
	if strings.TrimSpace(parts[1]) == "" || strings.TrimSpace(parts[3]) == "" {
		return "", "", false
	}
	return parts[3], path.Join(parts[0], parts[1], parts[2], parts[3]) + "/", true
}

func (s *StorageService) generateObjectURL(key string) string {
	if s.publicURL != "" {
		return strings.TrimRight(s.publicURL, "/") + "/" + strings.TrimLeft(key, "/")
	}

	if s.endpoint != "" {
		return strings.TrimRight(s.endpoint, "/") + "/" + strings.TrimLeft(s.bucket, "/") + "/" + strings.TrimLeft(key, "/")
	}

	return fmt.Sprintf("https://%s.s3.%s.amazonaws.com/%s", s.bucket, s.region, strings.TrimLeft(key, "/"))
}
