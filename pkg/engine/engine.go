package engine

import (
	"context"
	"sync"
	"time"

	"github.com/go-pkgz/lgr"
)

type Config struct {
	// Путь к директории из которой будут скачиваться файлы
	InputPath string

	// Путь к директории в которую будут скачиваться файлы
	OutputPath string

	// Путь к директории для временных файлов
	TempPath string

	// Размер части файла в байтах
	PartitionSize int64

	// Количество потоков для скачивания файлов
	Concurrency int

	// Интервал сканирования файлов
	ScanEvery time.Duration
}

func New(log lgr.L, conf Config, q Queue, s Scanner, d Downloader, c Collector) *Engine {
	return &Engine{
		log:        log,
		config:     conf,
		queue:      q,
		scanner:    s,
		collector:  c,
		downloader: d,
	}
}

type Engine struct {
	log        lgr.L
	config     Config
	queue      Queue
	scanner    Scanner
	collector  Collector
	downloader Downloader
}

func (e *Engine) Start(ctx context.Context) {
	// Добавление новых файлов в очередь загрузки
	filesCH := e.makeScannerQueue(ctx, e.config.ScanEvery, e.config.InputPath)
	go e.addToFileQueue(ctx, filesCH)

	// Запуск процесса получения частей файлов
	partitionsCH := e.makePartitionsQueue(ctx, time.Second*10)
	go e.downloadParts(ctx, e.config.Concurrency, partitionsCH)

	// Запуск процесса сборки файлов и очистки очереди
	completeCH := e.makeCompleteFilesQueue(ctx, time.Minute*3)
	go e.collectFiles(ctx, completeCH)

	// Запуск процесса отслеживания прогресса загрузки файлов
	go e.progressPrinter(ctx, time.Second*30)
}

// Данный метод переодически запускает сканирование новых файлов в удаленном хранилище
func (e *Engine) makeScannerQueue(ctx context.Context, duration time.Duration, inputPath string) <-chan File {
	ch := make(chan File)

	go func() {
		defer close(ch)

		ticker := time.NewTicker(duration)
		e.log.Logf("[DEBUG] старт цикла сканирования файлов")

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				e.log.Logf("[DEBUG] запуск сканирования файлов")

				files, err := e.scanner.Scan(e.config, inputPath)
				if err != nil {
					e.log.Logf("[ERROR] не удалось провести сканирование файлов: %v", err)
					continue
				}

				e.log.Logf("[DEBUG] сканирование файлов завершено: %d файлов найдено", len(files))

				for _, file := range files {
					ch <- file
				}
			default:
				time.Sleep(time.Millisecond * 100)
			}
		}
	}()

	return ch
}

// Данный метод добавляет новые файлы в очередь
func (e *Engine) addToFileQueue(ctx context.Context, input <-chan File) {
	for file := range input {
		e.log.Logf("[DEBUG] проверка файла %s", file.Name)

		isDownloaded, err := e.collector.Check(file.Dest, file.Size)
		if err != nil {
			e.log.Logf("[ERROR] не удалось проверить наличие файла: %v", err)
			continue
		}

		if isDownloaded {
			e.log.Logf("[DEBUG] файл %s уже загружен", file.Name)
			continue
		}

		qFile, err := e.queue.Get(ctx, file.ID)
		if err != nil && err != ErrNotFound {
			e.log.Logf("[ERROR] не удалось проверить наличие файла в очереди: %v", err)
			continue
		}
		if err == nil {
			if qFile.Size == file.Size {
				e.log.Logf("[DEBUG] файл %s уже в очереди", file.Name)
				continue
			}
		}

		err = e.queue.Upsert(ctx, file)
		if err != nil {
			e.log.Logf("[ERROR] не удалось добавить файл в очередь: %v", err)
		}

		e.log.Logf("[INFO] файл %s успешно добавлен в очередь на загрузку", file.Name)
	}
}

// Данный метод запускает процесс поиска частей файлов для загрузки
func (e *Engine) makePartitionsQueue(ctx context.Context, duration time.Duration) <-chan Part {
	partitions := make(chan Part)

	go func() {
		defer close(partitions)

		ticker := time.NewTicker(duration)

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				e.log.Logf("[DEBUG] запуск поиска частей для загрузки")

				files, err := e.queue.List(ctx, 10, func(f File) bool {
					if len(f.Parts) == f.CompleteParts {
						e.log.Logf("[DEBUG] файл %s уже загружен", f.Name)
						return false
					}

					return true
				})

				if err != nil {
					e.log.Logf("[ERROR] не удалось получить список файлов из очереди: %v", err)
					continue
				}

				for _, file := range files {
					for _, part := range file.Parts {
						if part.Complete {
							continue
						}

						partitions <- part
					}
				}
			default:
				time.Sleep(time.Millisecond * 100)
			}
		}
	}()

	return partitions
}

// Данный метод запускает загрузку частей файлов из очереди
func (e *Engine) downloadParts(ctx context.Context, concurency int, input <-chan Part) {
	wg := sync.WaitGroup{}
	rl := make(chan struct{}, concurency)

	for part := range input {
		wg.Add(1)
		rl <- struct{}{}

		go func(part Part) {
			defer func() {
				wg.Done()
				<-rl
			}()

			err := e.downloader.DownloadWithRetry(part, 3)
			if err != nil {
				e.log.Logf("[ERROR] во время загрузки части файла %s произошла ошибка: %v", part.ID, err)
				return
			}

			err = e.queue.MarkPartComplete(ctx, part.FileID, part.ID)
			if err != nil {
				e.log.Logf("[ERROR] не удалось отметить часть файла %s как загруженную: %v", part.ID, err)
			}
		}(part)
	}

	wg.Wait()
}

// Метод запускает процесс загруженных файлов
func (e *Engine) makeCompleteFilesQueue(ctx context.Context, duration time.Duration) <-chan File {
	ch := make(chan File)

	go func() {
		defer close(ch)

		ticker := time.NewTicker(duration)

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				e.log.Logf("[DEBUG] Поиск успешно загруженных файлов")

				files, err := e.queue.List(ctx, 10, func(f File) bool {
					if len(f.Parts) != f.CompleteParts {
						return false
					}

					return true
				})

				if err != nil {
					e.log.Logf("[ERROR] не удалось получить список файлов из очереди: %v", err)
					continue
				}

				for _, file := range files {
					ch <- file
				}
			default:
				time.Sleep(time.Millisecond * 100)
			}
		}
	}()

	return ch
}

// Метод запускает процесс сборки файла и очистки очереди от завершенной задачи
func (e *Engine) collectFiles(ctx context.Context, input <-chan File) {
	for file := range input {
		err := e.collector.Collect(file)
		if err != nil {
			e.log.Logf("[ERROR] не удалось отметить файл %s как загруженный: %v", file.ID, err)
			continue
		}

		err = e.queue.Delete(ctx, file.ID)
		if err != nil {
			e.log.Logf("[ERROR] не удалось удалить завершенную задачу %s из очереди: %v", file.ID, err)
		}

		e.log.Logf("[INFO] файл %s успешно загружен", file.Name)
	}
}

// Метод запускает процесс отслеживания прогресса загрузки файлов
func (e *Engine) progressPrinter(ctx context.Context, duration time.Duration) {
	ticker := time.NewTicker(duration)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			var (
				totalParts    int
				completeParts int
			)

			_, err := e.queue.List(ctx, 10, func(f File) bool {
				totalParts += len(f.Parts)
				completeParts += f.CompleteParts
				return false
			})

			if err != nil {
				continue
			}

			if totalParts == 0 {
				continue
			}

			e.log.Logf("[INFO] Прогресс %d%% (%d/%d)", (completeParts*100)/totalParts, completeParts, totalParts)
		default:
			time.Sleep(time.Millisecond * 100)
		}
	}
}
