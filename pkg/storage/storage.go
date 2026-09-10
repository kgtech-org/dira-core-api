// Package storage stores uploaded media (dish photos, logos, avatars,
// vehicle photos) in S3-compatible object storage (MinIO in dev). Objects
// are public-read; documents persist the public URL.
package storage

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// Allowed upload kinds; the kind prefixes the object key.
var Kinds = map[string]bool{
	"dish": true, "store": true, "brand": true, "vehicle": true, "avatar": true, "feed": true,
	// Visuels du carrousel publicitaire (internal/banner). Images seules :
	// une bannière est une carte, pas une vidéo.
	"banner": true,
}

// AllowedTypes maps accepted IMAGE content types to file extensions.
var AllowedTypes = map[string]string{
	"image/jpeg":    ".jpg",
	"image/png":     ".png",
	"image/webp":    ".webp",
	"image/svg+xml": ".svg",
}

// VideoTypes maps accepted VIDEO content types to file extensions.
//
// Réservées au kind "feed" : une photo de plat ou un logo d'enseigne n'a
// aucune raison d'être une vidéo, et l'y autoriser ouvrirait un dépôt de
// gros fichiers sur tous les points d'entrée.
var VideoTypes = map[string]string{
	"video/mp4":       ".mp4",
	"video/webm":      ".webm",
	"video/quicktime": ".mov",
}

// MaxUploadBytes bounds a single image upload.
const MaxUploadBytes = 5 << 20 // 5 MiB

// MaxVideoBytes bounds a single feed video. Plus généreux que les images —
// une vidéo promo dure quelques secondes mais pèse des dizaines de mégaoctets
// — sans devenir un espace de stockage libre.
const MaxVideoBytes = 60 << 20 // 60 MiB

// Accepts tells whether a kind takes this content type, and with which
// extension. Point de décision UNIQUE : le handler HTTP et le stockage
// doivent appliquer la même règle, sinon l'un accepte ce que l'autre rejette.
func Accepts(kind, contentType string) (ext string, ok bool) {
	if ext, ok = AllowedTypes[contentType]; ok {
		return ext, true
	}
	if kind == "feed" {
		ext, ok = VideoTypes[contentType]
		return ext, ok
	}
	return "", false
}

// MaxBytesFor returns the size ceiling of a kind.
func MaxBytesFor(kind string) int64 {
	if kind == "feed" {
		return MaxVideoBytes
	}
	return MaxUploadBytes
}

type Config struct {
	Endpoint  string // host:port
	AccessKey string
	SecretKey string
	UseSSL    bool
	Bucket    string
	// PublicBaseURL is what browsers use to fetch objects
	// (e.g. http://localhost:9000). Defaults to the endpoint scheme+host.
	PublicBaseURL string
}

type Store struct {
	client *minio.Client
	bucket string
	public string
}

// New connects to the object store and ensures the bucket exists with a
// public-read policy (dev-friendly; production may front it with a CDN).
func New(ctx context.Context, cfg Config) (*Store, error) {
	client, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.UseSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("storage: connect: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	exists, err := client.BucketExists(ctx, cfg.Bucket)
	if err != nil {
		return nil, fmt.Errorf("storage: bucket check: %w", err)
	}
	if !exists {
		if err := client.MakeBucket(ctx, cfg.Bucket, minio.MakeBucketOptions{}); err != nil {
			return nil, fmt.Errorf("storage: make bucket: %w", err)
		}
	}
	policy := fmt.Sprintf(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":{"AWS":["*"]},"Action":["s3:GetObject"],"Resource":["arn:aws:s3:::%s/*"]}]}`, cfg.Bucket)
	if err := client.SetBucketPolicy(ctx, cfg.Bucket, policy); err != nil {
		return nil, fmt.Errorf("storage: bucket policy: %w", err)
	}

	public := strings.TrimSuffix(cfg.PublicBaseURL, "/")
	if public == "" {
		scheme := "http"
		if cfg.UseSSL {
			scheme = "https"
		}
		public = scheme + "://" + cfg.Endpoint
	}
	return &Store{client: client, bucket: cfg.Bucket, public: public}, nil
}

// Put streams an object and returns its public URL. The key is
// <kind>/<entity>/<uuid><ext>; entity may be empty.
func (s *Store) Put(ctx context.Context, kind, entity, contentType string, size int64, r io.Reader) (string, error) {
	ext, ok := Accepts(kind, contentType)
	if !ok {
		return "", fmt.Errorf("storage: unsupported content type %q for kind %q", contentType, kind)
	}
	key := kind + "/"
	if entity != "" {
		key += entity + "/"
	}
	key += uuid.NewString() + ext
	_, err := s.client.PutObject(ctx, s.bucket, key, r, size, minio.PutObjectOptions{ContentType: contentType})
	if err != nil {
		return "", fmt.Errorf("storage: put: %w", err)
	}
	return s.public + "/" + s.bucket + "/" + key, nil
}

// Key extracts the object key from a public URL produced by Put. Returns false
// for a URL that does not belong to this bucket — un worker ne doit pas suivre
// une URL arbitraire fournie par un appelant.
func (s *Store) Key(publicURL string) (string, bool) {
	prefix := s.public + "/" + s.bucket + "/"
	if !strings.HasPrefix(publicURL, prefix) {
		return "", false
	}
	key := strings.TrimPrefix(publicURL, prefix)
	if key == "" || strings.Contains(key, "..") {
		return "", false
	}
	return key, true
}

// Get opens an object previously stored by Put, addressed by its public URL.
func (s *Store) Get(ctx context.Context, publicURL string) (io.ReadCloser, error) {
	key, ok := s.Key(publicURL)
	if !ok {
		return nil, fmt.Errorf("storage: %q is not an object of bucket %s", publicURL, s.bucket)
	}
	obj, err := s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("storage: get %s: %w", key, err)
	}
	return obj, nil
}

// Remove deletes an object addressed by its public URL. Une URL étrangère au
// bucket est ignorée sans erreur : l'appelant nettoie au mieux, il ne doit pas
// échouer pour autant.
func (s *Store) Remove(ctx context.Context, publicURL string) error {
	key, ok := s.Key(publicURL)
	if !ok {
		return nil
	}
	if err := s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{}); err != nil {
		return fmt.Errorf("storage: remove %s: %w", key, err)
	}
	return nil
}
