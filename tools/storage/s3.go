package storage

import (
	"context"
	"fmt"
	"io"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// S3PantryState implements PantryState backed by S3

type S3PantryState struct {
	bucket string
	key    string
	s3     *s3.Client
}

func NewS3PantryState(s3Client *s3.Client, bucket, key string) *S3PantryState {
	return &S3PantryState{
		bucket: bucket,
		key:    key,
		s3:     s3Client,
	}
}

func (s *S3PantryState) Load(ctx context.Context) ([]byte, error) {
	resp, err := s.s3.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(s.key),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get pantry object from S3: %w", err)
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

// S3RecipeState implements RecipeState backed by S3

type S3RecipeState struct {
	bucket string
	key    string
	s3     *s3.Client
}

func NewS3RecipeState(s3Client *s3.Client, bucket, key string) *S3RecipeState {
	return &S3RecipeState{
		bucket: bucket,
		key:    key,
		s3:     s3Client,
	}
}

func (s *S3RecipeState) Load(ctx context.Context) ([]byte, error) {
	resp, err := s.s3.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(s.key),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get recipe object from S3: %w", err)
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}
