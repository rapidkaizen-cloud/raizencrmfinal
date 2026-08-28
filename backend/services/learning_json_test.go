package services

import (
	"context"
	"strings"
	"testing"

	openai "github.com/sashabaranov/go-openai"
)

// balasan menyusun respons AI palsu dengan isi dan finish_reason tertentu.
func balasan(content string, finish openai.FinishReason) openai.ChatCompletionResponse {
	return openai.ChatCompletionResponse{
		Choices: []openai.ChatCompletionChoice{{
			Message:      openai.ChatCompletionMessage{Content: content},
			FinishReason: finish,
		}},
	}
}

// TestAIJSONInto memastikan jalur retry menutup kasus nyata yang bikin AI Learning
// gagal: model menghabiskan jatah token untuk reasoning internal, membalas 200
// dengan content kosong dan finish_reason "length".
func TestAIJSONInto(t *testing.T) {
	asli := aiComplete
	defer func() { aiComplete = asli }()

	type hasil struct {
		Nama string `json:"nama"`
	}

	t.Run("percobaan kosong lalu berhasil", func(t *testing.T) {
		var budgetTerpakai []int
		aiComplete = func(_ context.Context, _ []openai.ChatCompletionMessage, maxTokens int, _ float32) (openai.ChatCompletionResponse, error) {
			budgetTerpakai = append(budgetTerpakai, maxTokens)
			if len(budgetTerpakai) == 1 {
				return balasan("", openai.FinishReasonLength), nil
			}
			return balasan("```json\n{\"nama\":\"budi\"}\n```", openai.FinishReasonStop), nil
		}

		var out hasil
		if err := aiJSONInto(nil, 0.3, []int{100, 900}, &out); err != nil {
			t.Fatalf("harusnya berhasil di percobaan kedua, malah: %v", err)
		}
		if out.Nama != "budi" {
			t.Fatalf("isi tidak terurai, dapat %q", out.Nama)
		}
		if len(budgetTerpakai) != 2 || budgetTerpakai[0] != 100 || budgetTerpakai[1] != 900 {
			t.Fatalf("budget harus naik bertahap, dapat %v", budgetTerpakai)
		}
	})

	t.Run("JSON terpotong dicoba ulang", func(t *testing.T) {
		panggilan := 0
		aiComplete = func(_ context.Context, _ []openai.ChatCompletionMessage, _ int, _ float32) (openai.ChatCompletionResponse, error) {
			panggilan++
			if panggilan == 1 {
				return balasan(`{"nama":"bu`, openai.FinishReasonLength), nil
			}
			return balasan(`{"nama":"budi"}`, openai.FinishReasonStop), nil
		}

		var out hasil
		if err := aiJSONInto(nil, 0.3, []int{100, 900}, &out); err != nil {
			t.Fatalf("harusnya pulih setelah JSON terpotong, malah: %v", err)
		}
		if out.Nama != "budi" {
			t.Fatalf("isi tidak terurai, dapat %q", out.Nama)
		}
	})

	t.Run("selalu kosong: error menyebut sebabnya", func(t *testing.T) {
		aiComplete = func(_ context.Context, _ []openai.ChatCompletionMessage, _ int, _ float32) (openai.ChatCompletionResponse, error) {
			return balasan("", openai.FinishReasonLength), nil
		}

		var out hasil
		err := aiJSONInto(nil, 0.3, []int{100, 900}, &out)
		if err == nil {
			t.Fatal("harusnya error kalau semua percobaan kosong")
		}
		// Pesan lama "unexpected end of JSON input (raw: )" tidak menjelaskan apa pun.
		// Yang baru wajib menyebut kosong + finish_reason supaya bisa didiagnosa.
		for _, wajib := range []string{"kosong", "length"} {
			if !strings.Contains(err.Error(), wajib) {
				t.Fatalf("pesan error harus memuat %q, dapat: %v", wajib, err)
			}
		}
	})
}
