// Adapted from vault/services/backend/internal/storage/minio.go.
// Simplified for dramalist: single bucket, proxy serving (no presigned PUT/GET),
// so MinIO does not need to be reachable by browsers directly.
package storage

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/url"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

const MediaBucket = "dramalist-media"

type Store struct {
	client    *minio.Client
	pubClient *minio.Client // for presigning with the public endpoint
}

// Connect creates a Store and ensures the media bucket exists.
// endpoint is the internal MinIO address (e.g. "minio:9000").
// publicURL is the externally-reachable MinIO base URL (e.g. "http://localhost:9000").
func Connect(endpoint, accessKey, secretKey, publicURL string) (*Store, error) {
	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: false,
		Region: "us-east-1",
	})
	if err != nil {
		return nil, fmt.Errorf("minio client: %w", err)
	}
	s := &Store{client: client}
	if err := s.ensureBucket(context.Background()); err != nil {
		return nil, fmt.Errorf("minio ensure bucket: %w", err)
	}

	// Build a separate client for presigning using the public URL so that
	// presigned URLs point to the externally-reachable endpoint.
	if publicURL != "" {
		pub, pubErr := url.Parse(publicURL)
		if pubErr == nil && pub.Host != "" {
			pubEndpoint := pub.Host
			secure := pub.Scheme == "https"
			// Strip any explicit port 80/443 that minio.New doesn't expect.
			pubEndpoint = strings.TrimSuffix(pubEndpoint, ":80")
			pubClient, pubErr := minio.New(pubEndpoint, &minio.Options{
				Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
				Secure: secure,
				Region: "us-east-1",
			})
			if pubErr == nil {
				s.pubClient = pubClient
			}
		}
	}
	if s.pubClient == nil {
		s.pubClient = client
	}
	return s, nil
}

func (s *Store) ensureBucket(ctx context.Context) error {
	exists, err := s.client.BucketExists(ctx, MediaBucket)
	if err != nil {
		return err
	}
	if !exists {
		if err := s.client.MakeBucket(ctx, MediaBucket, minio.MakeBucketOptions{}); err != nil {
			return err
		}
		log.Printf("storage: created bucket %q", MediaBucket)
	}
	return nil
}

// MediaKeyPrefix returns the shared key prefix for all variants of an image:
// {entityType}/{entityID}/{mediaType}/{id}_
func MediaKeyPrefix(entityType, entityID, mediaType, id string) string {
	return fmt.Sprintf("%s/%s/%s/%s_", entityType, entityID, mediaType, id)
}

// MediaKeyVariant builds the full object key for a sized WebP variant:
// {entityType}/{entityID}/{mediaType}/{id}_{size}.webp
// size is one of "thumb", "medium", "large".
func MediaKeyVariant(entityType, entityID, mediaType, id, size string) string {
	return MediaKeyPrefix(entityType, entityID, mediaType, id) + size + ".webp"
}

// PutObject streams r into the media bucket at key.
// size should be the exact byte length; pass -1 to use streaming (slightly less efficient).
func (s *Store) PutObject(ctx context.Context, key, contentType string, size int64, r io.Reader) error {
	_, err := s.client.PutObject(ctx, MediaBucket, key, r, size, minio.PutObjectOptions{
		ContentType: contentType,
	})
	return err
}

// GetObject returns a streaming reader and the object's content-type.
func (s *Store) GetObject(ctx context.Context, key string) (io.ReadCloser, string, error) {
	obj, err := s.client.GetObject(ctx, MediaBucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, "", err
	}
	info, err := obj.Stat()
	if err != nil {
		obj.Close()
		return nil, "", err
	}
	return obj, info.ContentType, nil
}

// DeleteObject removes key from the media bucket.
func (s *Store) DeleteObject(ctx context.Context, key string) error {
	return s.client.RemoveObject(ctx, MediaBucket, key, minio.RemoveObjectOptions{})
}

// PresignedPutURL generates a presigned PUT URL for the given bucket and key.
// The URL uses the public endpoint so it is accessible from outside the cluster.
func (s *Store) PresignedPutURL(ctx context.Context, bucket, key string, expiry time.Duration) (string, error) {
	u, err := s.pubClient.PresignedPutObject(ctx, bucket, key, expiry)
	if err != nil {
		return "", fmt.Errorf("presign put object: %w", err)
	}
	return u.String(), nil
}
