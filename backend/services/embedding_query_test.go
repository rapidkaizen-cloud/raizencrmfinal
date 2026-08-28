package services

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	openai "github.com/sashabaranov/go-openai"
)

// stubEmbeddingAPI mengarahkan client embedding ke server palsu dan menghitung
// berapa kali API dipanggil. Return-nya = fungsi restore untuk defer.
func stubEmbeddingAPI(t *testing.T, calls *int32) func() {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(calls, 1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"object": "list",
			"model":  "test-embedding",
			"data": []map[string]any{
				{"object": "embedding", "index": 0, "embedding": []float32{0.1, 0.2, 0.3}},
			},
		})
	}))

	embMu.Lock()
	prevClient, prevModel, prevDims, prevEnabled := embClient, embModel, embDims, embEnabled
	cfg := openai.DefaultConfig("test-key")
	cfg.BaseURL = srv.URL
	embClient = openai.NewClientWithConfig(cfg)
	embModel = "test-embedding"
	embDims = 0
	embEnabled = true
	embMu.Unlock()

	return func() {
		embMu.Lock()
		embClient, embModel, embDims, embEnabled = prevClient, prevModel, prevDims, prevEnabled
		embMu.Unlock()
		srv.Close()
	}
}

// Satu pesan masuk dipakai retrieval knowledge DAN katalog produk. Vektor query-nya
// harus dihitung sekali saja, bukan sekali per bagian retrieval.
func TestQueryVectorEmbedsOnce(t *testing.T) {
	var calls int32
	defer stubEmbeddingAPI(t, &calls)()

	qv := newQueryVector("berapa harga kaos polos")
	first := qv.Vec()  // dipakai selectKnowledgeAdvanced
	second := qv.Vec() // dipakai productKnowledgeContext
	if len(first) != 3 || len(second) != 3 {
		t.Fatalf("vektor query kosong: first=%v second=%v", first, second)
	}
	if n := atomic.LoadInt32(&calls); n != 1 {
		t.Fatalf("API embedding dipanggil %d kali, mau 1", n)
	}
}

// Retrieval yang tidak pernah butuh vektor tidak boleh membakar kuota embedding.
func TestQueryVectorLazyTidakDipanggilBilaTakDipakai(t *testing.T) {
	var calls int32
	defer stubEmbeddingAPI(t, &calls)()

	_ = newQueryVector("pesan yang retrievalnya berhenti lebih awal")
	if n := atomic.LoadInt32(&calls); n != 0 {
		t.Fatalf("API embedding dipanggil %d kali padahal vektor tak pernah diminta", n)
	}
}

// Jalur tanpa embedding (mis. test & fallback keyword) mengoper nil — jangan panic.
func TestQueryVectorNilAman(t *testing.T) {
	var qv *queryVector
	if vec := qv.Vec(); vec != nil {
		t.Fatalf("qv nil harus mengembalikan nil, dapat %v", vec)
	}
}

// Embedding nonaktif → Vec() nil supaya pemanggil jatuh ke keyword, bukan error.
func TestQueryVectorEmbeddingNonaktif(t *testing.T) {
	embMu.Lock()
	prevEnabled, prevClient := embEnabled, embClient
	embEnabled, embClient = false, nil
	embMu.Unlock()
	defer func() {
		embMu.Lock()
		embEnabled, embClient = prevEnabled, prevClient
		embMu.Unlock()
	}()

	if vec := newQueryVector("halo").Vec(); vec != nil {
		t.Fatalf("embedding nonaktif harus nil, dapat %v", vec)
	}
}
