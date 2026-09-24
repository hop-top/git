package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseComposePs(t *testing.T) {
	want := []statusService{
		{Service: "db", Name: "p-db-1", State: "running"},
		{Service: "web", Name: "p-web-1", State: "exited"},
	}

	t.Run("json array", func(t *testing.T) {
		ps := `[{"Service":"web","Name":"p-web-1","State":"exited"},{"Service":"db","Name":"p-db-1","State":"running"}]`
		assert.Equal(t, want, parseComposePs(ps))
	})
	t.Run("json lines", func(t *testing.T) {
		ps := "{\"Service\":\"web\",\"Name\":\"p-web-1\",\"State\":\"exited\"}\n" +
			"{\"Service\":\"db\",\"Name\":\"p-db-1\",\"State\":\"running\"}\n"
		assert.Equal(t, want, parseComposePs(ps))
	})
	t.Run("empty renders as no services", func(t *testing.T) {
		assert.Empty(t, parseComposePs(""))
		assert.Empty(t, parseComposePs("[]"))
	})
	t.Run("unparseable lines skipped", func(t *testing.T) {
		ps := "not json\n{\"Service\":\"db\",\"Name\":\"p-db-1\",\"State\":\"running\"}"
		assert.Equal(t, want[:1], parseComposePs(ps))
	})
}
