// wordcounter_test.go
package internal

import (
	"bytes"
	"context"
	"math/rand"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ========== Тесты для splitWordsBytes (ASCII mode) ==========

func TestSplitWordsBytes(t *testing.T) {
	tests := []struct {
		name          string
		input         []byte
		caseSensitive bool
		expected      []string
	}{
		{
			name:          "Простые слова ASCII",
			input:         []byte("Hello world"),
			caseSensitive: true,
			expected:      []string{"Hello", "world"},
		},
		{
			name:          "Знаки препинания игнорируются",
			input:         []byte("Hello, world! How?"),
			caseSensitive: true,
			expected:      []string{"Hello", "world", "How"},
		},
		{
			name:          "Приведение к нижнему регистру",
			input:         []byte("Hello HELLO hello"),
			caseSensitive: false,
			expected:      []string{"hello", "hello", "hello"},
		},
		{
			name:          "Цифры и подчёркивания",
			input:         []byte("test123 foo_bar v2.0"),
			caseSensitive: true,
			expected:      []string{"test123", "foo_bar", "v2", "0"},
		},
		{
			name:          "Пустой вход",
			input:         []byte(""),
			caseSensitive: true,
			expected:      []string{},
		},
		{
			name:          "Только пробелы",
			input:         []byte("   \t\n  "),
			caseSensitive: true,
			expected:      []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := splitWordsBytes(tt.input, tt.caseSensitive)
			if !equalSlices(result, tt.expected) {
				t.Errorf("got %v, want %v", result, tt.expected)
			}
		})
	}
}

// ========== Тесты для splitWordsUnicode (UTF-8 mode) ==========

func TestSplitWordsUnicode(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		caseSensitive bool
		expected      []string
	}{
		{
			name:          "Русский текст без учета регистра",
			input:         "Привет мир! Привет.",
			caseSensitive: false,
			expected:      []string{"привет", "мир", "привет"},
		},
		{
			name:          "Смешанный регистр кириллицы",
			input:         "Мама мыла раму",
			caseSensitive: true,
			expected:      []string{"Мама", "мыла", "раму"},
		},
		{
			name:          "Эмодзи и спецсимволы",
			input:         "Hello 👋 world 🌍!",
			caseSensitive: false,
			expected:      []string{"hello", "world"},
		},
		{
			name:          "Цифры и Unicode",
			input:         "Тест123 версия_2.0",
			caseSensitive: false,
			expected:      []string{"тест123", "версия_2", "0"},
		},
		{
			name:          "Слова с дефисами",
			input:         "красно-синий, из-за, какой-то",
			caseSensitive: false,
			expected:      []string{"красно-синий", "из-за", "какой-то"},
		},
		{
			name:          "Дефисы в начале и конце слов",
			input:         "-test- -hello-",
			caseSensitive: true,
			expected:      []string{"test", "hello"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := splitWordsUnicode(tt.input, tt.caseSensitive)
			if !equalSlices(result, tt.expected) {
				t.Errorf("got %v, want %v", result, tt.expected)
			}
		})
	}
}

// ========== Тесты для mergeMaps ==========

func TestMergeMaps(t *testing.T) {
	tests := []struct {
		name     string
		dest     map[string]int
		src      map[string]int
		expected map[string]int
	}{
		{
			name:     "Объединение с пересечением",
			dest:     map[string]int{"hello": 5, "world": 3},
			src:      map[string]int{"hello": 2, "go": 4},
			expected: map[string]int{"hello": 7, "world": 3, "go": 4},
		},
		{
			name:     "Пустая src",
			dest:     map[string]int{"a": 1},
			src:      map[string]int{},
			expected: map[string]int{"a": 1},
		},
		{
			name:     "Пустая dest",
			dest:     map[string]int{},
			src:      map[string]int{"b": 2},
			expected: map[string]int{"b": 2},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mergeMaps(tt.dest, tt.src)
			if !equalMaps(tt.dest, tt.expected) {
				t.Errorf("got %v, want %v", tt.dest, tt.expected)
			}
		})
	}
}

// ========== Тесты для getTopWords ==========

func TestGetTopWords(t *testing.T) {
	tests := []struct {
		name     string
		input    map[string]int
		n        int
		expected []WordCount
	}{
		{
			name:  "Топ-3 из 5 слов",
			input: map[string]int{"hello": 10, "world": 8, "go": 6, "test": 4, "code": 2},
			n:     3,
			expected: []WordCount{
				{"hello", 10},
				{"world", 8},
				{"go", 6},
			},
		},
		{
			name:  "Запрос больше чем есть слов",
			input: map[string]int{"a": 1, "b": 2},
			n:     10,
			expected: []WordCount{
				{"b", 2},
				{"a", 1},
			},
		},
		{
			name:     "Пустая мапа",
			input:    map[string]int{},
			n:        5,
			expected: []WordCount{},
		},
		{
			name:  "Сортировка по алфавиту при равных частотах",
			input: map[string]int{"zebra": 5, "apple": 5, "mango": 5},
			n:     3,
			expected: []WordCount{
				{"apple", 5},
				{"mango", 5},
				{"zebra", 5},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getTopWords(tt.input, tt.n)
			if len(result) != len(tt.expected) {
				t.Fatalf("got %d items, want %d", len(result), len(tt.expected))
			}
			for i := range result {
				if result[i] != tt.expected[i] {
					t.Errorf("[%d] got %v, want %v", i, result[i], tt.expected[i])
				}
			}
		})
	}
}

// ========== Тесты для isWhitespace и isWordByte ==========

func TestIsWhitespace(t *testing.T) {
	tests := []struct {
		b        byte
		expected bool
	}{
		{' ', true}, {'\t', true}, {'\n', true}, {'\r', true},
		{'a', false}, {'1', false}, {'.', false}, {'_', false},
	}
	for _, tt := range tests {
		if got := isWhitespace(tt.b); got != tt.expected {
			t.Errorf("isWhitespace(%q) = %v, want %v", tt.b, got, tt.expected)
		}
	}
}

func TestIsWordByte(t *testing.T) {
	tests := []struct {
		b        byte
		expected bool
	}{
		{'a', true}, {'Z', true}, {'0', true}, {'_', true}, {'-', true},
		{' ', false}, {'!', false}, {'.', false}, {0x00, false},
	}
	for _, tt := range tests {
		if got := isWordByte(tt.b); got != tt.expected {
			t.Errorf("isWordByte(%q) = %v, want %v", tt.b, got, tt.expected)
		}
	}
}

func TestIsWordRune(t *testing.T) {
	tests := []struct {
		r        rune
		expected bool
	}{
		{'а', true}, {'Я', true}, {'5', true}, {'_', true}, {'-', true},
		{' ', false}, {'!', false}, {'.', false}, {'👋', false},
	}
	for _, tt := range tests {
		if got := isWordRune(tt.r); got != tt.expected {
			t.Errorf("isWordRune(%q) = %v, want %v", tt.r, got, tt.expected)
		}
	}
}

// ========== Интеграционные тесты ==========

func TestWorkerBasic(t *testing.T) {
	ctx := context.Background()
	chunks := make(chan []byte, 2)
	results := make(chan map[string]int, 2)
	var processed atomic.Int64
	var metrics Metrics
	config := &Config{CaseSensitive: false, UnicodeMode: false}

	var wg sync.WaitGroup
	wg.Add(1)
	go worker(ctx, &wg, 1, chunks, results, &processed, &metrics, config)

	chunks <- []byte("Hello hello HELLO")
	chunks <- []byte("world WORLD")
	close(chunks)

	// Собираем результаты
	go func() {
		wg.Wait()
		close(results)
	}()

	// Проверяем результаты
	finalResult := make(map[string]int)
	for result := range results {
		mergeMaps(finalResult, result)
	}

	if finalResult["hello"] != 3 {
		t.Errorf("expected hello=3, got %d", finalResult["hello"])
	}
	if finalResult["world"] != 2 {
		t.Errorf("expected world=2, got %d", finalResult["world"])
	}
}

func TestCountWordsEndToEnd(t *testing.T) {
	// Создаём временный файл с тестовыми данными
	content := []byte("The quick brown fox jumps over the lazy dog. The fox was quick.")
	tmpFile, err := os.CreateTemp("", "wordcounter_*.txt")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	if _, err := tmpFile.Write(content); err != nil {
		t.Fatalf("Failed to write to temp file: %v", err)
	}

	config := &Config{
		FilePath:      tmpFile.Name(),
		NumWorkers:    2,
		ChunkSize:     32,
		ShowProgress:  false,
		CaseSensitive: false,
		UnicodeMode:   false,
	}

	ctx := context.Background()
	freq, uniqueCount, err := countWords(ctx, config)
	if err != nil {
		t.Fatalf("countWords failed: %v", err)
	}

	if freq["the"] != 3 {
		t.Errorf("expected 'the'=3, got %d", freq["the"])
	}
	if freq["fox"] != 2 {
		t.Errorf("expected 'fox'=2, got %d", freq["fox"])
	}
	if uniqueCount < 8 {
		t.Errorf("expected at least 8 unique words, got %d", uniqueCount)
	}
}

func TestContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Немедленная отмена

	chunks := make(chan []byte, 1)
	results := make(chan map[string]int, 1)
	var processed atomic.Int64
	var metrics Metrics
	config := &Config{CaseSensitive: false}

	var wg sync.WaitGroup
	wg.Add(1)
	done := make(chan struct{})

	go func() {
		worker(ctx, &wg, 1, chunks, results, &processed, &metrics, config)
		close(done)
	}()

	// Воркер должен завершиться быстро при отменённом контексте
	select {
	case <-done:
		// Успех
	case <-time.After(100 * time.Millisecond):
		t.Error("worker did not respect context cancellation")
	}
}

func TestDeterminism(t *testing.T) {
	// Создаём тестовый файл
	content := []byte("The quick brown fox jumps over the lazy dog. The fox was quick. " +
		"This is a test file with multiple lines.\n" +
		"It contains various words and punctuation! " +
		"Lorem ipsum dolor sit amet, consectetur adipiscing elit.")

	tmpFile, err := os.CreateTemp("", "determinism_*.txt")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	if _, err := tmpFile.Write(content); err != nil {
		t.Fatalf("Failed to write to temp file: %v", err)
	}

	config := &Config{
		FilePath:      tmpFile.Name(),
		NumWorkers:    4,
		ChunkSize:     32,
		ShowProgress:  false,
		CaseSensitive: false,
		UnicodeMode:   false,
	}

	// Запускаем несколько раз
	var results []map[string]int
	for i := range 5 {
		ctx := context.Background()
		result, _, err := countWords(ctx, config)
		if err != nil {
			t.Fatalf("countWords failed on run %d: %v", i+1, err)
		}
		results = append(results, result)
	}

	// Проверяем, что все результаты одинаковы
	for i := 1; i < len(results); i++ {
		if !equalMaps(results[0], results[i]) {
			t.Errorf("Results differ between run 1 and run %d", i+1)

			// Детальный вывод различий
			for word, count := range results[0] {
				if results[i][word] != count {
					t.Logf("Word %q: run1=%d, run%d=%d", word, count, i+1, results[i][word])
				}
			}
		}
	}
}

// ========== Бенчмарки ==========

func BenchmarkSplitWordsBytes(b *testing.B) {
	text := []byte("This is a sample text with many words to test the performance. " +
		"It contains punctuation, and various words for benchmarking! " +
		"Lorem ipsum dolor sit amet, consectetur adipiscing elit. ")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = splitWordsBytes(text, false)
	}
}

func BenchmarkSplitWordsUnicode(b *testing.B) {
	text := "Это тестовый текст на русском языке для бенчмарка. " +
		"Он содержит слова, знаки препинания и разный регистр! " +
		"Lorem ipsum dolor sit amet, consectetur adipiscing elit. "

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = splitWordsUnicode(text, false)
	}
}

func BenchmarkMergeMaps(b *testing.B) {
	dest := map[string]int{"word1": 1, "word2": 2, "word3": 3}
	src := map[string]int{"word2": 3, "word3": 4, "word4": 5}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mergeMaps(dest, src)
		// Сброс для честного бенчмарка
		for k := range src {
			dest[k] -= src[k]
			if dest[k] == 0 {
				delete(dest, k)
			}
		}
	}
}

func BenchmarkGetTopWords(b *testing.B) {
	wordFreq := make(map[string]int)
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	for range 10000 {
		wordFreq[randomString(8)] = r.Intn(100)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = getTopWords(wordFreq, 10)
	}
}

func BenchmarkReadAndSplitFile(b *testing.B) {
	// Создаём тестовый файл 1MB
	content := bytes.Repeat([]byte("The quick brown fox jumps over the lazy dog. "), 20000)
	tmpFile, err := os.CreateTemp("", "bench_*.txt")
	if err != nil {
		b.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	if _, err := tmpFile.Write(content); err != nil {
		b.Fatalf("Failed to write to temp file: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Переоткрываем файл для каждого прохода
		file, err := os.Open(tmpFile.Name())
		if err != nil {
			b.Fatalf("Failed to open file: %v", err)
		}

		chunks := make(chan []byte, 4)
		errChan := make(chan error, 1)

		go func() {
			errChan <- readAndSplitFile(context.Background(), file, 4096, chunks)
			close(chunks)
		}()

		// Потребляем чанки
		for range chunks {
		}

		if err := <-errChan; err != nil {
			b.Errorf("readAndSplitFile failed: %v", err)
		}

		file.Close()
	}
}

// ========== Вспомогательные функции ==========

func equalSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func equalMaps(a, b map[string]int) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func randomString(n int) string {
	letters := []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	s := make([]rune, n)
	for i := range s {
		s[i] = letters[r.Intn(len(letters))]
	}
	return string(s)
}
