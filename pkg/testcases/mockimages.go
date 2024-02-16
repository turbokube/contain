package testcases

import (
	"encoding/hex"
	"math/rand"
	"time"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/types"
)

var src = rand.New(rand.NewSource(time.Now().UnixNano()))

func NewMockHash(hash string) v1.Hash {
	if hash == "" {
		hash = "sha256:" + RandomHex(64)
	}
	h, err := v1.NewHash(hash)
	if err != nil {
		panic(err)
	}
	return h
}

type MockDescribable struct {
	hash      v1.Hash
	mediaType types.MediaType
	size      int64
}

func NewMockDescribable(hash string, t types.MediaType, s int64) MockDescribable {
	h := NewMockHash(hash)
	return MockDescribable{
		hash:      h,
		mediaType: t,
		size:      s,
	}
}

func (m MockDescribable) Digest() (v1.Hash, error) {
	return m.hash, nil
}

func (m MockDescribable) MediaType() (types.MediaType, error) {
	return m.mediaType, nil
}

func (m MockDescribable) Size() (int64, error) {
	return m.size, nil
}

// RandomHex returns a random hexadecimal string of length n.
func RandomHex(n int) string {
	b := make([]byte, (n+1)/2)

	if _, err := src.Read(b); err != nil {
		panic(err)
	}

	return hex.EncodeToString(b)[:n]
}
