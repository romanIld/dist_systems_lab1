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

Docker с Compose v2 (`docker compose`). Go и прочие инструменты
локально не нужны - сборка и запуск идут в контейнерах.

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

Python (только для графиков, к Go-коду отношения не имеет):

| Пакет | Зачем |
|-------|-------|
| `matplotlib` | Построение PNG-графиков RTT и Throughput в `scripts/plot.py`. |

Всё остальное - стандартная библиотека Go: `net`, `bufio`, `context`,
`os/exec`, `encoding/csv`, `encoding/json`, `syscall` (вызов
`QueryPerformanceCounter` в `internal/hrtime`), `flag`, `log`, `sync`,
`sync/atomic`, `time`.

## Сборка и запуск (Docker)

[`Dockerfile`](Dockerfile) собирает все бинарники в образ `lab1-echo`. Каждый
сценарий описан отдельным compose-файлом:

| Файл | Сценарий |
|------|----------|
| [`compose.test.yaml`](compose.test.yaml) | `go vet` + юнит-тесты |
| [`compose.smoke-threading.yaml`](compose.smoke-threading.yaml) | сервер 1.1 в отдельном контейнере, клиент проверяет его по сети |
| [`compose.smoke-async.yaml`](compose.smoke-async.yaml) | то же для сервера 1.2 |
| [`compose.smoke-grpc.yaml`](compose.smoke-grpc.yaml) | то же для сервера 1.3 |

```
docker compose -f compose.test.yaml run --rm test

docker compose -f compose.smoke-threading.yaml up --build --abort-on-container-exit --exit-code-from client
docker compose -f compose.smoke-async.yaml up --build --abort-on-container-exit --exit-code-from client
docker compose -f compose.smoke-grpc.yaml up --build --abort-on-container-exit --exit-code-from client
```

В smoke-сценарии сервер стартует первым (`depends_on`), после завершения
клиента оба контейнера останавливаются, код выхода команды - код выхода
клиента (0 - все ответы получены). Удалить остановленные контейнеры:
`docker compose -f <файл> down`.

Клиент печатает статистику (пример для `compose.smoke-threading.yaml`):

```
Messages sent: 1000
Average RTT: 0.608 ms
Min RTT: 0.039 ms
Max RTT: 4.428 ms
P95 RTT: 1.348 ms
Total time: 63.30 ms
Throughput: 15798.9 msg/s
```

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
