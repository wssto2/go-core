package product

import (
	"bytes"
	"context"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"log/slog"

	"github.com/disintegration/imaging"
	storage "github.com/wssto2/go-core/storage"
	"github.com/wssto2/go-core/event"
)

const (
	thumbnailSize = 300 // 300×300 px, centre-cropped
	mediumSize    = 800 // 800px on the longest side, aspect-ratio preserved
)

// imageWorker subscribes to ProductImageUploadedEvent and generates image
// variants (thumbnail + medium) in a background goroutine.
// It reads the original from storage, processes it with the imaging library,
// writes the variants back, and updates the product row with the new URLs.
type imageWorker struct {
	repo    Repository
	store   storage.Driver
	log     *slog.Logger
}

func newImageWorker(repo Repository, store storage.Driver, log *slog.Logger) (*imageWorker, error) {
	return &imageWorker{repo: repo, store: store, log: log}, nil
}

func (w *imageWorker) Name() string { return "product-image-processor" }

// Subscribe registers the event handler on the bus. Call this during module
// registration — before the bus starts dispatching events.
func (w *imageWorker) Subscribe(bus event.Bus) error {
	return bus.Subscribe(ProductImageUploadedEvent{}, w.handle)
}

func (w *imageWorker) Run(ctx context.Context) error {
	<-ctx.Done()
	return nil
}

func (w *imageWorker) handle(ctx context.Context, e any) error {
	ev, ok := e.(ProductImageUploadedEvent)
	if !ok {
		return fmt.Errorf("imageWorker: unexpected event type %T", e)
	}

	w.log.InfoContext(ctx, "image processing started", "product_id", ev.ProductID, "key", ev.OriginalKey)

	if err := w.repo.UpdateImage(ctx, ev.ProductID, ev.OriginalKey, "", ImageStatusProcessing); err != nil {
		return fmt.Errorf("imageWorker: mark processing: %w", err)
	}

	thumbKey, mediumKey, err := w.process(ctx, ev.OriginalKey)
	if err != nil {
		w.log.ErrorContext(ctx, "image processing failed", "product_id", ev.ProductID, "err", err)
		_ = w.repo.UpdateImage(ctx, ev.ProductID, ev.OriginalKey, "", ImageStatusFailed)
		return err
	}

	if err := w.repo.UpdateImage(ctx, ev.ProductID, mediumKey, thumbKey, ImageStatusDone); err != nil {
		return fmt.Errorf("imageWorker: mark done: %w", err)
	}

	w.log.InfoContext(ctx, "image processing done",
		"product_id", ev.ProductID,
		"thumbnail", thumbKey,
		"medium", mediumKey,
	)
	return nil
}

// process reads the original, produces thumbnail and medium variants, stores
// them, and returns their storage keys.
func (w *imageWorker) process(ctx context.Context, originalKey string) (thumbKey, mediumKey string, err error) {
	// Read the original once; decode into an image.Image.
	rc, err := w.store.Get(ctx, originalKey)
	if err != nil {
		return "", "", fmt.Errorf("read original: %w", err)
	}
	defer rc.Close()

	raw, err := io.ReadAll(rc)
	if err != nil {
		return "", "", fmt.Errorf("read original bytes: %w", err)
	}

	src, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return "", "", fmt.Errorf("decode image: %w", err)
	}

	// Derive keys from the original (replace "original" prefix with variant name).
	dir := storageDir(originalKey)
	ext := storageExt(originalKey)
	thumbKey = dir + "/thumbnail" + ext
	mediumKey = dir + "/medium" + ext

	// Thumbnail: square centre-crop at thumbnailSize.
	thumb := imaging.Fill(src, thumbnailSize, thumbnailSize, imaging.Center, imaging.Lanczos)
	if err := w.encodeAndStore(ctx, thumbKey, ext, thumb); err != nil {
		return "", "", fmt.Errorf("store thumbnail: %w", err)
	}

	// Medium: fit within mediumSize×mediumSize preserving aspect ratio.
	medium := imaging.Fit(src, mediumSize, mediumSize, imaging.Lanczos)
	if err := w.encodeAndStore(ctx, mediumKey, ext, medium); err != nil {
		return "", "", fmt.Errorf("store medium: %w", err)
	}

	return thumbKey, mediumKey, nil
}

func (w *imageWorker) encodeAndStore(ctx context.Context, key, ext string, img image.Image) error {
	var buf bytes.Buffer
	format := imaging.JPEG
	mime := "image/jpeg"
	if ext == ".png" {
		format = imaging.PNG
		mime = "image/png"
	}
	if err := imaging.Encode(&buf, img, format); err != nil {
		return err
	}
	b := buf.Bytes()
	return w.store.Put(ctx, key, bytes.NewReader(b), int64(len(b)), mime)
}

// storageDir returns everything before the last "/" in a key.
func storageDir(key string) string {
	for i := len(key) - 1; i >= 0; i-- {
		if key[i] == '/' {
			return key[:i]
		}
	}
	return ""
}

// storageExt returns the file extension of a storage key (e.g. ".jpg").
func storageExt(key string) string {
	for i := len(key) - 1; i >= 0; i-- {
		if key[i] == '.' {
			return key[i:]
		}
		if key[i] == '/' {
			break
		}
	}
	return ".jpg"
}
