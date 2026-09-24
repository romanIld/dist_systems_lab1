# Лабораторная работа 1 - Эхо-сервер с разными моделями коммуникации

Система "Эхо" в трёх архитектурах сетевого взаимодействия и инструмент их
сравнения. Клиент шлёт строки `Hello X\n`, сервер отвечает
`ECHO: <строка> [t=<timestamp>]\n`; клиент измеряет RTT каждого сообщения.

| Часть | Модель | Сервер | Клиент |
|-------|--------|--------|--------|
| 1.1 | Блокирующая, поток на соединение | [`cmd/server-threading`](cmd/server-threading) | [`cmd/client-tcp`](cmd/client-tcp) |
| 1.2 | Асинхронная, событийный цикл | [`cmd/server-async`](cmd/server-async) | [`cmd/client-tcp`](cmd/client-tcp) |
| 1.3 | Потоковый RPC (gRPC, bidirectional) | [`cmd/server-grpc`](cmd/server-grpc) | [`cmd/client-grpc`](cmd/client-grpc) |
| 1.4 | Сравнительный анализ | [`cmd/benchmark`](cmd/benchmark) + [`scripts/plot.py`](scripts/plot.py) |

Результаты и выводы - в [REPORT.md](REPORT.md).

## Требования

- Go 1.25+ (версия зафиксирована в `go.mod`, тулчейн подтягивается автоматически)
- Python 3.9+ с `matplotlib` - только для построения графиков
  (`pip install -r scripts/requirements.txt`)
- gRPC-код в [`internal/echopb`](internal/echopb) уже сгенерирован и лежит в
  репозитории. Перегенерация (необязательна) требует `buf`, `protoc-gen-go`,
  `protoc-gen-go-grpc`:
  ```
  go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
  go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
  go install github.com/bufbuild/buf/cmd/buf@latest
  buf generate
  ```

## Внешние библиотеки

Проект по возможности использует только стандартную библиотеку Go. Единственные
прямые зависимости - gRPC и Protobuf, и они обязательны по условию Задания 1.3
(RPC-фреймворк с потоковой передачей + `.proto`-контракт + сгенерированный код,
стандартной библиотекой это не покрывается):

| Библиотека | Где | Зачем |
|------------|-----|-------|
| `google.golang.org/grpc` | `cmd/server-grpc`, `cmd/client-grpc` (1.3) | Реализация gRPC (RPC поверх HTTP/2): двунаправленный поток `EchoStream`, регистрация сервиса, клиентское соединение. |
| `google.golang.org/protobuf` | `internal/echopb` (сген.), `cmd/server-grpc` | Рантайм Protocol Buffers: бинарная сериализация сообщений и тип `timestamppb.Timestamp` для серверной метки времени (требование 3.4). |

Транзитивно с ними приходят (`// indirect` в `go.mod`):
`golang.org/x/{net,sys,text}` и `google.golang.org/genproto/googleapis/rpc`.
Полный список с версиями - в `go.mod` / `go.sum`.

Что реализовано на стандартной библиотеке вместо сторонних пакетов:

- **Событийный сервер 1.2** (`cmd/server-async`) - собственный реактор на пакете
  `net`: пул из `-loops` горутин, неблокирующие чтения через короткий read
  deadline, без горутины на соединение.
- **Замер CPU/RSS** (`internal/procstat`) - системные вызовы напрямую:
  `GetProcessTimes` + `GetProcessMemoryInfo` через `syscall` на Windows,
  чтение `/proc/<pid>/{stat,statm}` на Linux.
- **Высокоточный таймер** (`internal/hrtime`) - `QueryPerformanceCounter` через
  `syscall` на Windows, `time` на прочих ОС.

Инструменты сборки (в бинарники не входят, нужны только для `buf generate`):

| Инструмент | Зачем |
|------------|-------|
| `github.com/bufbuild/buf` | Компиляция `proto/echo.proto` без установки `protoc`. |
| `google.golang.org/protobuf/cmd/protoc-gen-go` | Генерация Go-структур сообщений из контракта. |
| `google.golang.org/grpc/cmd/protoc-gen-go-grpc` | Генерация Go-кода клиента/сервера gRPC из контракта. |

Python (только для графиков, к Go-коду отношения не имеет):

| Пакет | Зачем |
|-------|-------|
| `matplotlib` | Построение PNG-графиков RTT и Throughput в `scripts/plot.py`. |

Всё остальное - стандартная библиотека Go: `net`, `bufio`, `context`,
`os/exec`, `encoding/csv`, `encoding/json`, `syscall` (вызов
`QueryPerformanceCounter` в `internal/hrtime`), `flag`, `log`, `sync`,
`sync/atomic`, `time`.

## Сборка

```
# Windows (PowerShell)
powershell -File scripts/build.ps1

# Linux/macOS
for c in server-threading server-async server-grpc client-tcp client-grpc benchmark; do
  go build -o bin/ ./cmd/$c
done
```

Бинарники складываются в `bin/`.

> На Windows каждый бинарник собирается отдельной командой и с повторными
> попытками: `go build ./...` для нескольких `main`-пакетов конфликтует на общем
> временном файле, а антивирус может кратко удерживать только что записанный
> `.exe`.

## Запуск вручную

```
# 1.1 - блокирующий сервер
bin/server-threading -addr :9101
bin/client-tcp -addr 127.0.0.1:9101 -messages 1000 -concurrency 100

# 1.2 - асинхронный сервер (4 событийных цикла)
bin/server-async -addr :9102 -loops 4
bin/client-tcp -addr 127.0.0.1:9102 -messages 1000 -concurrency 500

# 1.3 - gRPC потоковый сервер
bin/server-grpc -addr :9103
bin/client-grpc -addr 127.0.0.1:9103 -messages 1000 -streams 100
```

Каждый клиент печатает статистику:

```
Messages sent: 100000
Average RTT: 1.234 ms
Min RTT: 0.085 ms
Max RTT: 12.900 ms
P95 RTT: 3.100 ms
Total time: 2100.50 ms
Throughput: 47600.0 msg/s
```

Флаг `-json` у клиентов выдаёт ту же статистику одной JSON-строкой (используется
оркестратором `benchmark`).

## Полный прогон измерений (Задание 1.4)

```
# Windows: сборка + тесты + бенчмарк + графики
powershell -File scripts/all.ps1

# или только бенчмарк
bin/benchmark -bin bin -out results \
  -messages 10,100,1000 -concurrency 1,10,50,100,200

# графики из results/summary.csv
python scripts/plot.py
```

Оркестратор для каждого подхода поднимает сервер, прогоняет сетку
`messages x concurrency`, снимает CPU/RSS процесса сервера и пишет:

- `results/raw/bench-<timestamp>.csv` - все замеры;
- `results/summary.csv` - последний прогон (вход для графиков);
- `results/logs/<approach>.log` - вывод серверов;
- `results/*.png` - графики RTT и Throughput.

Если порт занят зависшим сервером от прошлого запуска:
`powershell -File scripts/kill-servers.ps1`.

## Тесты

```
go test ./...
```

Покрывают формат протокола (`internal/protocol`) и расчёт статистики
`avg/min/max/p95/throughput` (`internal/metrics`).

## Проверка в Docker (Linux)

[`Dockerfile`](Dockerfile) собирает все бинарники в образ `lab1-echo`. Каждый
сценарий описан отдельным compose-файлом:

| Файл | Сценарий |
|------|----------|
| [`compose.test.yaml`](compose.test.yaml) | `go vet` + юнит-тесты |
| [`compose.smoke-threading.yaml`](compose.smoke-threading.yaml) | сервер 1.1 в отдельном контейнере, клиент проверяет его по сети |
| [`compose.smoke-async.yaml`](compose.smoke-async.yaml) | то же для сервера 1.2 |
| [`compose.smoke-grpc.yaml`](compose.smoke-grpc.yaml) | то же для сервера 1.3 |
| [`compose.bench.yaml`](compose.bench.yaml) | полная сетка бенчмарка, результаты в `results/linux/` |

```
docker compose -f compose.test.yaml run --rm test

docker compose -f compose.smoke-threading.yaml up --build --abort-on-container-exit --exit-code-from client
docker compose -f compose.smoke-async.yaml up --build --abort-on-container-exit --exit-code-from client
docker compose -f compose.smoke-grpc.yaml up --build --abort-on-container-exit --exit-code-from client

docker compose -f compose.bench.yaml run --rm benchmark
```

В smoke-сценарии сервер стартует первым (`depends_on`), после завершения
клиента оба контейнера останавливаются, код выхода команды - код выхода
клиента (0 - все ответы получены). Удалить остановленные контейнеры:
`docker compose -f <файл> down`.
Бенчмарк сам запускает серверы и снимает их CPU/RSS по PID, поэтому работает
в одном контейнере. Параметры передаются после имени сервиса, например
`docker compose -f compose.bench.yaml run --rm benchmark benchmark -bin /app/bin -out /results -concurrency 1,10`
(в Git Bash - с `MSYS_NO_PATHCONV=1`, иначе пути `/app/bin` будут искажены).

## Структура

```
proto/echo.proto           Контракт gRPC (bidirectional streaming, Protobuf)
internal/protocol          Текстовый протокол ECHO для 1.1 и 1.2
internal/metrics           Сбор RTT и сводная статистика
internal/procstat          Семплер CPU/RSS процесса сервера
internal/echopb            Сгенерированный код gRPC
cmd/server-threading       1.1 - горутина на соединение, блокирующий I/O
cmd/server-async           1.2 - событийный цикл (реактор на пакете net), неблокирующий I/O
cmd/server-grpc            1.3 - gRPC-сервис EchoStream
cmd/client-tcp             Клиент для 1.1 и 1.2
cmd/client-grpc            Клиент для 1.3
cmd/benchmark              1.4 - оркестратор прогонов
scripts/                   Сборка, графики, очистка (PowerShell) + plot.py
```
# dist_systems_lab1
