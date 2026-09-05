// Package photo is this app's photo domain over the shared ingest pipeline in
// go-photo: which kinds of photo it stores and which poses a progress shot
// can be tagged with.
package photo

import (
	"io"
	"os"
	"strconv"

	gophoto "github.com/anchoo2kewl/go-photo"
)

// ErrUnsupportedType is returned when the bytes are not an image we accept.
var ErrUnsupportedType = gophoto.ErrUnsupportedType

// ErrTooLarge is returned when an upload exceeds the configured ceiling.
var ErrTooLarge = gophoto.ErrTooLarge

// Kinds of photo the app stores.
const (
	KindProgress    = "progress"
	KindFood        = "food"
	KindIngredients = "ingredients"
)

// Poses a progress photo can be taken from.
const (
	PoseFront = "front"
	PoseSide  = "side"
	PoseBack  = "back"
)

// ValidPose reports whether pose is one we store. Empty means untagged.
func ValidPose(pose string) bool {
	switch pose {
	case "", PoseFront, PoseSide, PoseBack:
		return true
	}
	return false
}

// ValidKind reports whether kind is one we store.
func ValidKind(kind string) bool {
	switch kind {
	case KindProgress, KindFood, KindIngredients:
		return true
	}
	return false
}

// Store writes images beneath a single root directory.
type Store struct {
	inner *gophoto.Store
	opt   gophoto.Options
}

// NewStore creates a Store rooted at dir.
func NewStore(dir string, maxEdge, thumbEdge int) (*Store, error) {
	if maxEdge <= 0 {
		maxEdge = gophoto.DefaultMaxEdge
	}
	if thumbEdge <= 0 {
		thumbEdge = 320
	}
	opt := gophoto.Options{
		MaxEdge: maxEdge, ThumbEdge: thumbEdge, Quality: 82,
		// The re-encode strips EXIF, and a progress photo's EXIF is the GPS
		// coordinates of somebody's bedroom.
		ForceRecompress: true,
	}
	inner, err := gophoto.NewStore(dir, opt)
	if err != nil {
		return nil, err
	}
	return &Store{inner: inner, opt: opt}, nil
}

// Root returns the directory the store writes into.
func (s *Store) Root() string { return s.inner.Root() }

// Saved describes an image that has been written to disk.
type Saved struct {
	RelPath   string
	ThumbPath string
	Mime      string
	Width     int
	Height    int
	Bytes     int64
	SHA256    string
}

// Save decodes r, downscales it, writes it plus a thumbnail under
// {userID}/{kind}/{yyyy}/{mm}/ and reports what it stored.
func (s *Store) Save(r io.Reader, userID int64, kind string, maxBytes int64) (*Saved, error) {
	opt := s.opt
	opt.MaxBytes = maxBytes
	raw, err := io.ReadAll(io.LimitReader(r, maxBytes+1))
	if err != nil {
		return nil, err
	}
	return s.SaveBytes(raw, userID, kind, maxBytes)
}

// SaveBytes is Save over bytes already in memory.
func (s *Store) SaveBytes(raw []byte, userID int64, kind string, maxBytes int64) (*Saved, error) {
	opt := s.opt
	opt.MaxBytes = maxBytes
	img, err := gophoto.Process(raw, "", opt)
	if err != nil {
		return nil, err
	}
	saved, err := s.inner.SaveImage(img, gophoto.Dated(strconv.FormatInt(userID, 10), kind))
	if err != nil {
		return nil, err
	}
	return &Saved{
		RelPath:   saved.RelPath,
		ThumbPath: saved.ThumbPath,
		Mime:      saved.ContentType,
		Width:     saved.Width,
		Height:    saved.Height,
		Bytes:     saved.Bytes(),
		SHA256:    saved.SHA256,
	}, nil
}

// Open returns a reader for a stored path, rejecting anything that tries to
// escape the root.
func (s *Store) Open(rel string) (*os.File, error) { return s.inner.Open(rel) }

// Remove deletes stored images, ignoring missing files.
func (s *Store) Remove(paths ...string) { _ = s.inner.Remove(paths...) }

// SaveDocument stores a file that is not an image, byte for byte, under the
// same dated, path-safe naming rule as a photo.
func (s *Store) SaveDocument(raw []byte, userID int64, kind, filename string, maxBytes int64) (*Saved, error) {
	opt := s.opt
	opt.MaxBytes = maxBytes
	opt.KeepUnsupported = true
	if int64(len(raw)) > maxBytes {
		return nil, gophoto.ErrTooLarge
	}
	doc, err := gophoto.Process(raw, filename, opt)
	if err != nil {
		return nil, err
	}
	saved, err := s.inner.SaveImage(doc, gophoto.Dated(strconv.FormatInt(userID, 10), kind))
	if err != nil {
		return nil, err
	}
	return &Saved{RelPath: saved.RelPath, Mime: saved.ContentType, Bytes: saved.Bytes(), SHA256: saved.SHA256}, nil
}
