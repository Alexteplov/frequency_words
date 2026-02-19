.PHONY: build run test bench clean create-test-file help

BINARY_NAME=wordcounter
BUILD_FULDER=build
GO=go

# Определяем ОС
ifeq ($(OS),Windows_NT)
    DETECTED_OS := Windows
    RM=del /q /f /s build\*.* >nul & for /d /r build %i in (*) do rd /s /q "%i"
    EXEC=$(BUILD_FULDER)/$(BINARY_NAME).exe
else
    DETECTED_OS := $(shell uname)
    RM="rm -rf" && $RM build/* build/.* 2>/dev/null || true
    EXEC=$(BUILD_FULDER)/./$(BINARY_NAME)
endif

build:
	$(GO) build -ldflags="-s -w" -o $(EXEC) cmd/main.go

run: build
ifeq ($(DETECTED_OS),Windows)
	.\$(EXEC) -file test/test.txt -workers 4 -chunk-size 65536 -progress
else
	$(EXEC) -file test/test.txt -workers 4 -chunk-size 65536 -progress
endif

runru: build
ifeq ($(DETECTED_OS),Windows)
	.\$(EXEC) -file test/tolstoy.txt -workers 4 -chunk-size 65536 -unicode
else
	$(EXEC) -file test/tolstoy.txt -workers 4 -chunk-size 65536 -unicode
endif

test:
	$(GO) test -v -cover ./...

bench:
	$(GO) test -bench=. -benchmem ./...

clean:
	$(RM) $(EXEC)
	$(RM) *.test *.testdata test.txt

# Разделяем создание тестового файла на две отдельные команды
create-test-file:
	@echo Создание тестового файла для Linux/Mac...
	@for i in {1..100000}; do echo "The quick brown fox $$i jumps over the lazy dog."; done > test/test.txt
	@ls -la test.txt

help:
	@echo "Доступные команды:"
	@echo "  build           - сборка приложения"
	@echo "  run             - запуск с тестовым файлом" 
	@echo "  runru             - запуск с русским текстом и тестовым файлом tolstoy.txt" 
	@echo "  test            - запуск тестов"
	@echo "  bench           - запуск бенчмарков"
	@echo "  clean           - очистка"
	@echo "  create-test-file - создание тестового файла"
	@echo "  help            - показать эту справку"