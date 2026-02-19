package internal

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unicode"
)

// WordCount представляет слово и его частоту
type WordCount struct {
	Word  string
	Count int
}

// Config содержит конфигурацию приложения
type Config struct {
	FilePath      string
	NumWorkers    int
	ChunkSize     int
	ShowProgress  bool
	CaseSensitive bool
	UnicodeMode   bool // Поддержка Unicode (медленнее, но корректно для кириллицы)
}

// Metrics содержит метрики выполнения
type Metrics struct {
	TotalWords     int64
	TotalChunks    int64
	ProcessingTime time.Duration
	BytesPerSecond float64
}

func Run() {
	log.SetPrefix(">> WordCounter | ")
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds)

	config := parseFlags()

	if err := validateConfig(config); err != nil {
		log.Fatalf("Ошибка конфигурации: %v", err)
	}

	// Обработка Ctrl+C
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		log.Println("\nПолучен сигнал прерывания, завершаем работу...")
		cancel()
	}()

	log.Printf("Запуск обработки файла: %s", config.FilePath)
	log.Printf("Параметры: workers=%d, chunkSize=%d KB", config.NumWorkers, config.ChunkSize/1024)

	startTime := time.Now()

	wordFreq, uniqueCount, err := countWords(ctx, config)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			log.Println("Обработка прервана пользователем")
			os.Exit(1)
		}
		log.Fatalf("Ошибка при подсчете слов: %v", err)
	}

	topWords := getTopWords(wordFreq, 10)
	printResults(topWords, uniqueCount, time.Since(startTime))
}

func parseFlags() *Config {
	config := &Config{}

	flag.StringVar(&config.FilePath, "file", "", "Путь к текстовому файлу для анализа (обязательный)")
	flag.IntVar(&config.NumWorkers, "workers", runtime.NumCPU(), "Количество горутин")
	flag.IntVar(&config.ChunkSize, "chunk-size", 64*1024, "Размер чанка в байтах")
	flag.BoolVar(&config.ShowProgress, "progress", true, "Показывать прогресс")
	flag.BoolVar(&config.CaseSensitive, "case-sensitive", false, "Учитывать регистр")
	flag.BoolVar(&config.UnicodeMode, "unicode", false, "Поддерживать Unicode (кириллица, эмодзи и т.д.)")

	flag.Parse()

	if config.FilePath == "" {
		fmt.Fprintf(os.Stderr, "Ошибка: не указан путь к файлу\n\n")
		flag.Usage()
		os.Exit(1)
	}

	return config
}

func validateConfig(config *Config) error {
	if config.NumWorkers <= 0 {
		return fmt.Errorf("количество воркеров должно быть > 0, получено: %d", config.NumWorkers)
	}
	if config.ChunkSize <= 0 {
		return fmt.Errorf("размер чанка должен быть > 0, получено: %d", config.ChunkSize)
	}
	if config.ChunkSize > 10*1024*1024 { // 10MB
		return fmt.Errorf("размер чанка слишком большой (макс. 10MB): %d", config.ChunkSize)
	}
	return nil
}

// countWords возвращает (частоты слов, количество уникальных слов, ошибку)
func countWords(ctx context.Context, config *Config) (map[string]int, int, error) {
	file, err := os.Open(config.FilePath)
	if err != nil {
		return nil, 0, fmt.Errorf("не удалось открыть файл: %w", err)
	}
	defer file.Close()

	fileInfo, err := file.Stat()
	if err != nil {
		return nil, 0, fmt.Errorf("не удалось получить stat файла: %w", err)
	}
	fileSize := fileInfo.Size()
	log.Printf("Размер файла: %d байт (%.2f MB)", fileSize, float64(fileSize)/(1024*1024))

	chunks := make(chan []byte, config.NumWorkers*2)
	results := make(chan map[string]int, config.NumWorkers)
	done := make(chan struct{})

	var processedBytes atomic.Int64
	var metrics Metrics

	// Запуск воркеров
	var wg sync.WaitGroup
	for i := 0; i < config.NumWorkers; i++ {
		wg.Add(1)
		go worker(ctx, &wg, i+1, chunks, results, &processedBytes, &metrics, config)
	}

	// Сборщик результатов
	finalResult := make(map[string]int)
	var resultMutex sync.Mutex
	var collectWg sync.WaitGroup
	collectWg.Go(func() {
		for {
			select {
			case partialResult, ok := <-results:
				if !ok {
					return
				}
				resultMutex.Lock()
				mergeMaps(finalResult, partialResult)
				resultMutex.Unlock()
				atomic.AddInt64(&metrics.TotalChunks, 1)
			case <-ctx.Done():
				return
			}
		}
	})

	if config.ShowProgress {
		go showProgress(fileSize, &processedBytes, done, ctx)
	}

	err = readAndSplitFile(ctx, file, config.ChunkSize, chunks)
	close(chunks)

	wg.Wait()
	close(results)
	if config.ShowProgress {
		close(done)
	}
	collectWg.Wait()

	if err != nil && !errors.Is(err, context.Canceled) {
		return nil, 0, err
	}

	return finalResult, len(finalResult), nil
}

func readAndSplitFile(ctx context.Context, file *os.File, chunkSize int,
	chunks chan<- []byte) error {
	reader := bufio.NewReaderSize(file, chunkSize*2)
	buffer := make([]byte, chunkSize)
	remainder := make([]byte, 0, chunkSize)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		n, err := reader.Read(buffer)
		if err != nil {
			if errors.Is(err, io.EOF) {
				if len(remainder) > 0 {
					// Отправляем остаток как есть
					select {
					case chunks <- remainder:
					case <-ctx.Done():
						return ctx.Err()
					}
				}
				break
			}
			return fmt.Errorf("ошибка чтения: %w", err)
		}

		// Объединяем с остатком от предыдущего чанка
		data := make([]byte, len(remainder)+n)
		copy(data, remainder)
		copy(data[len(remainder):], buffer[:n])

		// Ищем последний разделитель
		lastSeparator := -1
		for i := len(data) - 1; i >= 0; i-- {
			if isWhitespace(data[i]) {
				lastSeparator = i
				break
			}
		}

		if lastSeparator != -1 {
			// Отправляем часть до разделителя
			chunk := make([]byte, lastSeparator)
			copy(chunk, data[:lastSeparator])

			select {
			case chunks <- chunk:
			case <-ctx.Done():
				return ctx.Err()
			}

			// Сохраняем остаток после разделителя
			remainder = make([]byte, len(data)-lastSeparator-1)
			copy(remainder, data[lastSeparator+1:])
		} else {
			// Нет разделителя - отправляем всё как есть
			chunk := make([]byte, len(data))
			copy(chunk, data)

			select {
			case chunks <- chunk:
			case <-ctx.Done():
				return ctx.Err()
			}

			remainder = remainder[:0]
		}
	}
	return nil
}
func isWhitespace(b byte) bool {
	return unicode.IsSpace(rune(b))
}

func worker(ctx context.Context, wg *sync.WaitGroup, id int,
	chunks <-chan []byte, results chan<- map[string]int,
	processedBytes *atomic.Int64, metrics *Metrics, config *Config) {
	defer wg.Done()
	log.Printf("Воркер %d запущен", id)
	for {
		select {
		case <-ctx.Done():
			log.Printf("Воркер %d: отмена", id)
			return
		case chunk, ok := <-chunks:
			if !ok {
				return
			}

			wordFreq := make(map[string]int)

			if config.UnicodeMode {
				// Медленный парсинг Unicode
				words := splitWordsUnicode(string(chunk), config.CaseSensitive)
				for _, word := range words {
					if word != "" {
						wordFreq[word]++
					}
				}
			} else {
				// Быстрый ASCII-парсинг (по умолчанию)
				words := splitWordsBytes(chunk, config.CaseSensitive)
				for _, word := range words {
					if word != "" {
						wordFreq[word]++
					}
				}
			}

			// Обновляем метрики
			bytesProcessed := int64(len(chunk))
			processedBytes.Add(bytesProcessed)
			atomic.AddInt64(&metrics.TotalWords, int64(len(wordFreq)))

			select {
			case results <- wordFreq:
			case <-ctx.Done():
				return
			}
		}
	}
}

// splitWordsBytes: быстрый парсер для ASCII (a-zA-Z0-9_-)
func splitWordsBytes(data []byte, caseSensitive bool) []string {
	words := make([]string, 0, 64)
	startIdx := -1

	for i, b := range data {
		if isWordByte(b) {
			if startIdx == -1 {
				startIdx = i
			}
		} else {
			if startIdx != -1 {
				word := data[startIdx:i]
				if !caseSensitive {
					word = bytes.ToLower(word)
				}
				words = append(words, string(word))
				startIdx = -1
			}
		}
	}

	// Последнее слово
	if startIdx != -1 {
		word := data[startIdx:]
		if !caseSensitive {
			word = bytes.ToLower(word)
		}
		words = append(words, string(word))
	}

	return words
}

// splitWordsUnicode: корректный парсер для UTF-8 (кириллица, эмодзи и т.д.)
func splitWordsUnicode(text string, caseSensitive bool) []string {
	words := make([]string, 0, 64)
	var wordBuf strings.Builder
	wordBuf.Grow(32) // Предварительное выделение памяти

	for _, r := range text {
		// Учитываем дефисы внутри слов (например, "красно-синий")
		if isWordRune(r) || (r == '-' && wordBuf.Len() > 0) {
			if !caseSensitive {
				r = unicode.ToLower(r)
			}
			wordBuf.WriteRune(r)
		} else {
			if wordBuf.Len() > 0 {
				// Обрезаем дефис в конце слова
				word := strings.Trim(wordBuf.String(), "-")
				if word != "" {
					words = append(words, word)
				}
				wordBuf.Reset()
			}
		}
	}

	if wordBuf.Len() > 0 {
		word := strings.Trim(wordBuf.String(), "-")
		if word != "" {
			words = append(words, word)
		}
	}
	return words
}

func isWordByte(b byte) bool {
	return (b >= 'a' && b <= 'z') ||
		(b >= 'A' && b <= 'Z') ||
		(b >= '0' && b <= '9') ||
		b == '_' || b == '-'
}

func isWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsNumber(r) || r == '_' || r == '-'
}

func mergeMaps(dest, src map[string]int) {
	// Оптимизированное слияние для больших мап
	for word, count := range src {
		if existing, ok := dest[word]; ok {
			dest[word] = existing + count
		} else {
			dest[word] = count
		}
	}
}

func showProgress(fileSize int64, processedBytes *atomic.Int64,
	done <-chan struct{}, ctx context.Context) {
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	startTime := time.Now()
	var lastProcessed int64
	var lastTime time.Time

	for {
		select {
		case <-ctx.Done():
			fmt.Println("\r Обработка прервана")
			return
		case <-done:
			processed := processedBytes.Load()
			fmt.Printf("\r Завершено! %d/%d байт (100%%) | Время: %v\n",
				processed, fileSize, time.Since(startTime).Round(time.Second))
			return
		case <-ticker.C:
			processed := processedBytes.Load()
			percent := float64(processed) / float64(fileSize) * 100
			elapsed := time.Since(startTime)
			elapsedSec := elapsed.Seconds()

			speed := 0.0
			if elapsedSec > 0 {
				speed = float64(processed) / elapsedSec / 1024 // KB/s
			}

			// Мгновенная скорость (за последние 200ms)
			instantSpeed := 0.0
			if !lastTime.IsZero() {
				instantSpeed = float64(processed-lastProcessed) / time.Since(lastTime).Seconds() / 1024
			}
			lastProcessed = processed
			lastTime = time.Now()

			fmt.Printf("\r Прогресс: %5.1f%% | %d/%d байт | ⚡%.0f KB/s | avg:%.0f KB/s | %v",
				percent, processed, fileSize, instantSpeed, speed, elapsed.Round(time.Second))
		}
	}
}

// getTopWords: для больших словарей можно оптимизировать через min-heap O(n log K)
func getTopWords(wordFreq map[string]int, n int) []WordCount {
	if len(wordFreq) == 0 {
		return []WordCount{}
	}

	words := make([]WordCount, 0, len(wordFreq))
	for word, count := range wordFreq {
		words = append(words, WordCount{Word: word, Count: count})
	}

	sort.Slice(words, func(i, j int) bool {
		if words[i].Count == words[j].Count {
			return words[i].Word < words[j].Word
		}
		return words[i].Count > words[j].Count
	})

	if len(words) > n {
		return words[:n]
	}
	return words
}

func printResults(topWords []WordCount, uniqueCount int, duration time.Duration) {
	fmt.Println("\n" + strings.Repeat("=", 50))
	fmt.Println("ТОП-10 САМЫХ ЧАСТЫХ СЛОВ")
	fmt.Println(strings.Repeat("=", 50))

	for i, wc := range topWords {
		fmt.Printf("%2d. %-20s %d\n", i+1, wc.Word, wc.Count)
	}

	fmt.Println(strings.Repeat("=", 50))
	fmt.Printf("Время выполнения: %v\n", duration)
	fmt.Printf("Всего уникальных слов: %d\n", uniqueCount)

	// Память
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	fmt.Printf("Использовано памяти: %.2f MB\n", float64(m.Alloc)/1024/1024)
}
