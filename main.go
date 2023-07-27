package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

type CheckInfo struct {
	URL        string  `json:"url"`
	CheckStart string  `json:"check_start"`
	Interval   float64 `json:"interval"`
	Timeout    float64 `json:"timeout"`
}

type CheckResult struct {
	SuccessRate   float64   `json:"success_rate"`
	ResponseTimes []int64   `json:"response_times"`
	CheckInfo     CheckInfo `json:"check_info"`
}

func main() {
	var url string
	var numRequests int
	var checkInterval time.Duration
	var checkTimeout time.Duration

	flag.StringVar(&url, "u", "", "URL тестируемого API")
	flag.IntVar(&numRequests, "n", 1, "Количество параллельных запросов в чеке")
	flag.DurationVar(&checkInterval, "i", time.Second, "Интервал между чеками")
	flag.DurationVar(&checkTimeout, "t", time.Second*10, "Таймаут одного чека")

	flag.Parse()

	if url == "" {
		log.Fatal("Необходимо указать URL тестируемого API")
	}

	log.Printf("Тестируемый URL: %s\n", url)
	log.Printf("Количество параллельных запросов в чеке: %d\n", numRequests)
	log.Printf("Интервал между чеками: %.2f s\n", checkInterval.Seconds())
	log.Printf("Таймаут одного чека: %.2f s\n", checkTimeout.Seconds())
	log.Println("------------------------------------------")

	// Создание директории для результатов, если её нет
	if err := os.MkdirAll("results", 0755); err != nil {
		log.Fatalf("Ошибка создания директории для результатов: %s\n", err)
	}

	// Имя файла == времени запуска чека
	currentTime := time.Now().Format("2006-01-02_15-04-05")
	resultsFilePath := "results/" + currentTime + ".json"
	resultsFile, err := os.OpenFile(resultsFilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		log.Fatalf("Ошибка открытия файла для результатов: %s\n", err)
	}
	defer resultsFile.Close()

	var mutex sync.Mutex // Мьютекс для синхронизации доступа к totalLatency

	// Канал для получения сигнала SIGINT (Ctrl+C)
	sigintCh := make(chan os.Signal, 1)
	signal.Notify(sigintCh, syscall.SIGINT)

	for {
		log.Println("Запуск чека...")

		ctx, cancel := context.WithTimeout(context.Background(), checkTimeout)

		// Обнуление totalLatency перед каждым чеком
		totalLatency := int64(0)

		results := make(chan int64, numRequests)
		var wg sync.WaitGroup

		for i := 0; i < numRequests; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				latency := makeRequest(ctx, url)
				if latency >= 0 {
					mutex.Lock()
					totalLatency += latency
					mutex.Unlock()
					results <- latency
				}
			}()
		}

		wg.Wait()
		close(results)

		successCount := 0
		var responseTimes []int64
		for result := range results {
			successCount++
			responseTimes = append(responseTimes, result)
		}

		successRate := float64(successCount) / float64(numRequests) * 100
		averageLatency := calculateAverageLatency(totalLatency, successCount)
		log.Printf("Успешных запросов: %.2f%%\n", successRate)
		log.Printf("Средняя задержка: %d мс\n", averageLatency)

		checkInfo := CheckInfo{
			URL:        url,
			CheckStart: time.Now().Format("2006-01-02T15:04:05Z07:00"),
			Interval:   checkInterval.Seconds(),
			Timeout:    checkTimeout.Seconds(),
		}

		checkResult := CheckResult{
			SuccessRate:   successRate,
			ResponseTimes: responseTimes,
			CheckInfo:     checkInfo,
		}

		// Сохранение результатов в файл
		if err := saveResultsToFile(resultsFile, checkResult); err != nil {
			log.Println("Ошибка сохранения результатов:", err)
		}

		log.Println("Чек завершен.")
		log.Println("------------------------------------------")
		cancel()

		// Ожидание сигнала SIGINT (Ctrl+C) или перед следующим чеком (плавный shutdown)
		select {
		case <-sigintCh:
			log.Println("Получен сигнал SIGINT. Завершение чека...")
			os.Exit(0)
		case <-time.After(checkInterval):
		}
	}
}

func makeRequest(ctx context.Context, url string) int64 {
	client := http.Client{}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return -1
	}

	startTime := time.Now()
	resp, err := client.Do(req.WithContext(ctx))
	if err != nil {
		return -1
	}
	defer resp.Body.Close()

	return time.Since(startTime).Nanoseconds() / int64(time.Millisecond)
}

func calculateAverageLatency(totalLatency int64, successCount int) int64 {
	if successCount == 0 {
		return 0
	}
	return totalLatency / int64(successCount)
}

func saveResultsToFile(file *os.File, result CheckResult) error {
	// Кодируем результаты в JSON
	resultJSON, err := json.Marshal(result)
	if err != nil {
		return err
	}

	// Разделение чеков используя новую строку
	resultJSON = append(resultJSON, '\n')

	_, err = file.Write(resultJSON)
	if err != nil {
		return err
	}

	return nil
}
