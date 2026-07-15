// Package objectstore abstracts where uploaded statements are staged before
// processing. In OpenShift this is the S3-compatible bucket provisioned by an
// ObjectBucketClaim (ODF / NooBaa); locally it falls back to a directory.
//
// Files are staging-only: the worker purges them immediately after
// extraction, per the PRD's privacy requirement.
package objectstore

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type Store interface {
	Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	Delete(ctx context.Context, key string) error
}

// --- S3 (ODF ObjectBucketClaim) ---

type S3Store struct {
	client *minio.Client
	bucket string
}

func NewS3Store(endpoint, accessKey, secretKey, bucket string, useSSL bool) (*S3Store, error) {
	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: useSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("connecting to object storage: %w", err)
	}
	return &S3Store{client: client, bucket: bucket}, nil
}

func (s *S3Store) Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error {
	_, err := s.client.PutObject(ctx, s.bucket, key, r, size,
		minio.PutObjectOptions{ContentType: contentType})
	return err
}

func (s *S3Store) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	obj, err := s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	// GetObject is lazy; force the first read so missing keys fail here.
	if _, err := obj.Stat(); err != nil {
		obj.Close()
		return nil, err
	}
	return obj, nil
}

func (s *S3Store) Delete(ctx context.Context, key string) error {
	return s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{})
}

// --- local filesystem fallback (development) ---

type LocalStore struct {
	dir string
}

func NewLocalStore(dir string) (*LocalStore, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, err
	}
	return &LocalStore{dir: dir}, nil
}

func (l *LocalStore) path(key string) string {
	// keys are server-generated UUID-based names, but stay defensive
	return filepath.Join(l.dir, filepath.Base(strings.ReplaceAll(key, "..", "")))
}

func (l *LocalStore) Put(_ context.Context, key string, r io.Reader, _ int64, _ string) error {
	f, err := os.Create(l.path(key))
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, r)
	return err
}

func (l *LocalStore) Get(_ context.Context, key string) (io.ReadCloser, error) {
	return os.Open(l.path(key))
}

func (l *LocalStore) Delete(_ context.Context, key string) error {
	err := os.Remove(l.path(key))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
