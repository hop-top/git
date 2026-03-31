package services

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExtractHopVolumeKeys(t *testing.T) {
	t.Run("extracts single reference", func(t *testing.T) {
		content := []byte(`services:
  web:
    volumes:
      - "${HOP_VOLUME_WEB_DATA}:/usr/share/nginx/html"
`)
		keys := extractHopVolumeKeys(content)
		assert.Equal(t, []string{"WEB_DATA"}, keys)
	})

	t.Run("extracts multiple references", func(t *testing.T) {
		content := []byte(`services:
  web:
    volumes:
      - "${HOP_VOLUME_WEB_DATA}:/usr/share/nginx/html"
  db:
    volumes:
      - "${HOP_VOLUME_DB_DATA}:/data"
`)
		keys := extractHopVolumeKeys(content)
		sort.Strings(keys)
		assert.Equal(t, []string{"DB_DATA", "WEB_DATA"}, keys)
	})

	t.Run("deduplicates repeated references", func(t *testing.T) {
		content := []byte(`${HOP_VOLUME_WEB_DATA} ${HOP_VOLUME_WEB_DATA}`)
		keys := extractHopVolumeKeys(content)
		assert.Equal(t, []string{"WEB_DATA"}, keys)
	})

	t.Run("ignores non-HOP_VOLUME vars", func(t *testing.T) {
		content := []byte(`${HOP_PORT_WEB} ${SOME_OTHER_VAR}`)
		keys := extractHopVolumeKeys(content)
		assert.Empty(t, keys)
	})

	t.Run("empty content returns nil", func(t *testing.T) {
		keys := extractHopVolumeKeys([]byte{})
		assert.Empty(t, keys)
	})
}
