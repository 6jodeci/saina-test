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
	ResponseCodes []int      `json:"response_codes"`
	CheckInfo     CheckInfo `json:"check_info"`
}

type Config struct {
	URL           string
	NumRequests   int
	CheckInterval time.Duration
	CheckTimeout  time.Duration
}

func main() {
	config := parseFlags()

	if err := createResultsDirectory(); err != nil {
		log.Fatalf("Ошибка создания директории для результатов: %s\n", err)
	}

	resultsFile, err := openResultsFile()
	if err != nil {
		log.Fatalf("Ошибка открытия файла для результатов: %s\n", err)
	}
	defer resultsFile.Close()

	sigintCh := make(chan os.Signal, 1)
	signal.Notify(sigintCh, syscall.SIGINT)

	runChecks(config, resultsFile, sigintCh)
}

func parseFlags() Config {
	var config Config

	flag.StringVar(&config.URL, "u", "", "URL тестируемого API")
	flag.IntVar(&config.NumRequests, "n", 1, "Количество параллельных запросов в чеке")
	flag.DurationVar(&config.CheckInterval, "i", time.Second, "Интервал между чеками")
	flag.DurationVar(&config.CheckTimeout, "t", time.Second*10, "Таймаут одного чека")

	flag.Parse()

	if config.URL == "" {
		log.Fatal("Необходимо указать URL тестируемого API")
	}

	log.Printf("Тестируемый URL: %s\n", config.URL)
	log.Printf("Количество параллельных запросов в чеке: %d\n", config.NumRequests)
	log.Printf("Интервал между чеками: %.2f s\n", config.CheckInterval.Seconds())
	log.Printf("Таймаут одного чека: %.2f s\n", config.CheckTimeout.Seconds())
	log.Println("------------------------------------------")

	return config
}

func createResultsDirectory() error {
	return os.MkdirAll("results", 0755)
}

func openResultsFile() (*os.File, error) {
	currentTime := time.Now().Format("2006-01-02_15-04-05")
	resultsFilePath := "results/" + currentTime + ".json"
	return os.OpenFile(resultsFilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
}

func runChecks(config Config, resultsFile *os.File, sigintCh chan os.Signal) {
	for {
		log.Println("Запуск чека...")

		ctx, cancel := context.WithTimeout(context.Background(), config.CheckTimeout)
		defer cancel()

		totalLatency, responseTimes, responseCodesArr := makeMultipleRequests(ctx, config.URL, config.NumRequests)

		successCount := len(responseTimes)
		successRate := calculateSuccessRate(successCount, config.NumRequests)
		averageLatency := calculateAverageLatency(totalLatency, successCount)

		log.Printf("Успешных запросов: %.2f%%\n", successRate)
		log.Printf("Средняя задержка: %d мс\n", averageLatency)

		checkInfo := CheckInfo{
			URL:        config.URL,
			CheckStart: time.Now().Format("2006-01-02T15:04:05Z07:00"),
			Interval:   config.CheckInterval.Seconds(),
			Timeout:    config.CheckTimeout.Seconds(),
		}

		checkResult := CheckResult{
			SuccessRate:   successRate,
			ResponseTimes: responseTimes,
			ResponseCodes: responseCodesArr,
			CheckInfo:     checkInfo,
		}

		if err := saveResultsToFile(resultsFile, checkResult); err != nil {
			log.Println("Ошибка сохранения результатов:", err)
		}

		log.Printf("Коды ответов от сервера: %v\n", responseCodesArr)
		log.Println("Чек завершен.")
		log.Println("------------------------------------------")

		// Ожидание сигнала SIGINT (Ctrl+C) или интервала между чеками
		select {
		case <-sigintCh:
			log.Println("Получен сигнал SIGINT. Завершение чека...")
			os.Exit(0)
		case <-time.After(config.CheckInterval):
		}
	}
}

func makeMultipleRequests(ctx context.Context, url string, numRequests int) (int64, []int64, []int) {
	var mutex sync.Mutex
	var totalLatency int64
	results := make(chan int64, numRequests)
	responseCodes := make(chan int, numRequests)
	var wg sync.WaitGroup

	for i := 0; i < numRequests; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			latency, responseCode := makeRequest(ctx, url)
			if latency >= 0 {
				mutex.Lock()
				totalLatency += latency
				mutex.Unlock()
				results <- latency
				responseCodes <- responseCode
			}
		}()
	}

	wg.Wait()
	close(results)
	close(responseCodes)

	var responseTimes []int64
	var responseCodesArr []int

	for result := range results {
		responseTimes = append(responseTimes, result)
	}

	for code := range responseCodes {
		responseCodesArr = append(responseCodesArr, code)
	}

	return totalLatency, responseTimes, responseCodesArr
}

func makeRequest(ctx context.Context, url string) (int64, int) {
	client := http.Client{}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return -1, -1
	}

	startTime := time.Now()
	resp, err := client.Do(req.WithContext(ctx))
	if err != nil {
		return -1, -1
	}
	defer resp.Body.Close()

	return time.Since(startTime).Nanoseconds() / int64(time.Millisecond), resp.StatusCode
}

func calculateAverageLatency(totalLatency int64, successCount int) int64 {
	if successCount == 0 {
		return 0
	}
	return totalLatency / int64(successCount)
}

func calculateSuccessRate(successCount, totalRequests int) float64 {
	return float64(successCount) / float64(totalRequests) * 100
}

func saveResultsToFile(file *os.File, result CheckResult) error {
	resultJSON, err := json.Marshal(result)
	if err != nil {
		return err
	}

	resultJSON = append(resultJSON, '\n')

	_, err = file.Write(resultJSON)
	return err
}
