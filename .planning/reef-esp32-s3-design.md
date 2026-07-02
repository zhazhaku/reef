# reef-client on ESP32-S3-ePaper-1.54 — 架构设计报告

> **作者**: reef-architect  
> **日期**: 2026-06-15  
> **目标平台**: ESP32-S3-DevKitC + Waveshare 1.54" ePaper (200×200)  
> **基础固件**: xiaozhi-ai (已部署, FreeRTOS)

---

## 1. reef 协议精简版（ESP32 最小消息集）

基于 `pkg/reef/protocol.go` (protocol version `reef-v1`)，ESP32 只需实现 5 种消息：

### 1.1 消息信封 (JSON)

```json
{
  "msg_type":  "register | task_completed | heartbeat | task_progress | task_failed",
  "task_id":   "uuid (task 相关消息必填，register/heartbeat 省略)",
  "timestamp": 1718400000123,
  "payload":   { ... }
}
```

### 1.2 register（WS 连接后第一帧，发给 server）

| 字段 | 类型 | 含义 | ESP32 示例 |
|------|------|------|-----------|
| `protocol_version` | string | 固定 `"reef-v1"` | `"reef-v1"` |
| `client_id` | string | 唯一 ID | `"esp32-epaper-01"` |
| `role` | string | 角色 | `"edge-device"` |
| `skills` | []string | 能力列表 | `["display_text","read_battery","set_led","capture_audio"]` |
| `capacity` | int | 并发任务数 | `1` |
| `providers` | []string | LLM provider (可省略) | `[]` |

### 1.3 register_ack / register_nack（server 回复）

```json
// register_ack
{ "client_id": "esp32-epaper-01", "server_time": 1718400000999 }

// register_nack
{ "reason": "invalid token" }
```

### 1.4 heartbeat（每 30s，client → server）

```json
{ "client_id": "esp32-epaper-01", "current_load": 0 }
```

### 1.5 task_dispatch（server → client，核心任务下发）

```json
{
  "task_id": "uuid-xxxx",
  "instruction": "display_text:你好世界",
  "parameters": {},
  "priority": 1,
  "timeout_ms": 30000,
  "attempt_limit": 1
}
```

`instruction` 格式采用前缀路由：`display_text:<内容>` / `read_battery` / `set_led:<颜色>` / `capture_audio:<秒数>`。

### 1.6 task_completed / task_failed（client → server）

```json
// task_completed
{ "task_id": "uuid-xxxx", "text": "OK, 电池 3.82V", "metadata": {"vbat": 3820} }

// task_failed
{ "task_id": "uuid-xxxx", "error_type": "execution_error", "error_message": "ePaper busy timeout" }
```

### 1.7 task_progress（可选，长任务进度上报）

```json
{ "task_id": "uuid-xxxx", "progress": 50.0, "status_text": "正在刷新屏幕…" }
```

---

## 2. C 实现要点（ESP-IDF + esp-websocket-client + cJSON）

### 2.1 依赖 & 配置

```c
// CMakeLists.txt 追加
REQUIRES esp_websocket_client cjson nvs_flash esp_http_client
```

### 2.2 WebSocket 连接 + x-reef-token header

```c
#include "esp_websocket_client.h"
#include "cJSON.h"

#define REEF_WS_URL     CONFIG_REEF_SERVER_URL   // e.g. "ws://192.168.1.100:9999/ws"
#define REEF_TOKEN      CONFIG_REEF_TOKEN
#define REEF_CLIENT_ID  CONFIG_REEF_CLIENT_ID

static esp_websocket_client_handle_t ws_client;

static void reef_ws_connect(void) {
    esp_websocket_client_config_t cfg = {
        .uri = REEF_WS_URL,
        .headers = "x-reef-token: " REEF_TOKEN "\r\n",  // ✅ 握手 header
        .reconnect_timeout_ms = 5000,
        .disable_auto_reconnect = false,
    };
    ws_client = esp_websocket_client_init(&cfg);
    esp_websocket_register_events(ws_client, WEBSOCKET_EVENT_ANY,
                                   reef_ws_event_handler, NULL);
    esp_websocket_client_start(ws_client);
}
```

### 2.3 注册帧构造

```c
static void reef_send_register(void) {
    cJSON *root = cJSON_CreateObject();
    cJSON_AddStringToObject(root, "msg_type", "register");

    cJSON *payload = cJSON_CreateObject();
    cJSON_AddStringToObject(payload, "protocol_version", "reef-v1");
    cJSON_AddStringToObject(payload, "client_id", REEF_CLIENT_ID);
    cJSON_AddStringToObject(payload, "role", "edge-device");
    cJSON_AddNumberToObject(payload, "capacity", 1);

    cJSON *skills = cJSON_AddArrayToObject(payload, "skills");
    cJSON_AddItemToArray(skills, cJSON_CreateString("display_text"));
    cJSON_AddItemToArray(skills, cJSON_CreateString("read_battery"));
    cJSON_AddItemToArray(skills, cJSON_CreateString("set_led"));
    cJSON_AddItemToArray(skills, cJSON_CreateString("capture_audio"));

    cJSON_AddItemToObject(root, "payload", payload);
    cJSON_AddNumberToObject(root, "timestamp", (double)time(NULL) * 1000);

    char *str = cJSON_PrintUnformatted(root);
    esp_websocket_client_send_text(ws_client, str, strlen(str), portMAX_DELAY);
    cJSON_free(str);
    cJSON_Delete(root);
}
```

### 2.4 消息接收 & 分发

```c
static void reef_on_message(const char *data, int len) {
    cJSON *msg = cJSON_ParseWithLength(data, len);
    if (!msg) return;

    cJSON *type = cJSON_GetObjectItem(msg, "msg_type");
    if (!type) { cJSON_Delete(msg); return; }

    const char *t = type->valuestring;

    if (strcmp(t, "register_ack") == 0) {
        ESP_LOGI(TAG, "注册成功");
    } else if (strcmp(t, "register_nack") == 0) {
        ESP_LOGE(TAG, "注册被拒");
        esp_websocket_client_stop(ws_client);
    } else if (strcmp(t, "task_dispatch") == 0) {
        cJSON *payload = cJSON_GetObjectItem(msg, "payload");
        cJSON *tid     = cJSON_GetObjectItem(msg, "task_id");
        cJSON *instr   = cJSON_GetObjectItem(payload, "instruction");
        if (tid && instr) {
            reef_execute_task(tid->valuestring, instr->valuestring);
        }
    }

    cJSON_Delete(msg);
}
```

### 2.5 心跳循环（FreeRTOS task）

```c
static void reef_heartbeat_task(void *arg) {
    while (1) {
        vTaskDelay(pdMS_TO_TICKS(30000));  // 30s interval

        cJSON *root = cJSON_CreateObject();
        cJSON_AddStringToObject(root, "msg_type", "heartbeat");
        cJSON *payload = cJSON_CreateObject();
        cJSON_AddStringToObject(payload, "client_id", REEF_CLIENT_ID);
        cJSON_AddNumberToObject(payload, "current_load", 0);
        cJSON_AddItemToObject(root, "payload", payload);
        cJSON_AddNumberToObject(root, "timestamp", (double)time(NULL) * 1000);
        char *str = cJSON_PrintUnformatted(root);
        esp_websocket_client_send_text(ws_client, str, strlen(str), pdMS_TO_TICKS(1000));
        cJSON_free(str);
        cJSON_Delete(root);
    }
}
```

---

## 3. 任务执行抽象（Hardware Task Executor）

ESP32-S3 无法跑 LLM，但可暴露 **hardware skills**。采用 instruction 前缀路由：

```
display_text:<内容>     → ePaper 全屏刷新显示
read_battery            → ADC 读取电池电压, 返回 "3.82 V"
set_led:<模式>          → 控制板载 LED (on/off/blink)
capture_audio:<秒>      → I2S 录音, 上传 PCM 数据或转 base64
get_temp                → 读取板载温度传感器
deep_sleep:<秒>         → 进入深度睡眠 N 秒后唤醒
```

### 3.1 Executor 骨架

```c
typedef void (*task_handler_t)(const char *task_id, const char *arg, char *result, size_t max_len);

typedef struct {
    const char *prefix;
    task_handler_t handler;
} skill_entry_t;

static void h_display_text(const char *tid, const char *arg, char *result, size_t max) {
    epaper_clear();
    epaper_draw_text(10, 10, arg, FONT_16);
    epaper_refresh();
    snprintf(result, max, "OK, displayed %d chars", (int)strlen(arg));
    reef_send_task_completed(tid, result);
}

static void h_read_battery(const char *tid, const char *arg, char *result, size_t max) {
    int mv = adc_read_battery_mv();
    snprintf(result, max, "battery=%d.%02dV", mv / 1000, mv % 1000);
    reef_send_task_completed(tid, result);
}

static skill_entry_t skills[] = {
    {"display_text:", h_display_text},
    {"read_battery",  h_read_battery},
    {"set_led:",      h_set_led},
    {"capture_audio:",h_capture_audio},
    {NULL, NULL},
};

void reef_execute_task(const char *task_id, const char *instruction) {
    char result_buf[512] = {0};
    for (int i = 0; skills[i].prefix; i++) {
        size_t plen = strlen(skills[i].prefix);
        if (strncmp(instruction, skills[i].prefix, plen) == 0) {
            const char *arg = instruction + plen;
            skills[i].handler(task_id, arg, result_buf, sizeof(result_buf));
            return;
        }
    }
    // Unknown skill
    reef_send_task_failed(task_id, "unknown_skill", instruction);
}
```

---

## 4. 与 xiaozhi-ai 共存

### 4.1 FreeRTOS 任务划分

| 任务名 | 优先级 | 栈 | 职责 |
|--------|--------|-----|------|
| `xiaozhi_main` | 5 | 8 KB | xiaozhi-ai 语音交互主循环（已有） |
| `reef_ws_task` | 4 | 6 KB | reef WebSocket 连接、收消息、发心跳 |
| `reef_exec_task` | 3 | 6 KB | 执行 reef server 下发的 hardware task |
| `mode_btn_task` | 2 | 2 KB | 轮询按键，长按切模式 |

### 4.2 共享资源互斥

```c
// --- 电子纸 (SPI) 互斥 ---
static SemaphoreHandle_t epaper_mutex;  // xSemaphoreCreateMutex()

void epaper_lock(void)   { xSemaphoreTake(epaper_mutex, pdMS_TO_TICKS(5000)); }
void epaper_unlock(void) { xSemaphoreGive(epaper_mutex); }

// 双方在操作 ePaper 前必须先 lock
// xiaozhi-ai: 显示对话文本时 lock
// reef:       display_text skill 调用时 lock

// --- Wi-Fi 无互斥需求（共用同一个 netif，FreeRTOS+TCPI/IP 栈线程安全）---
```

### 4.3 模式切换

```
[正常模式] → 长按 3s → [reef 独占模式] → 长按 3s → [正常模式]
```

```
正常模式:   xiaozhi-ai 前台运行, reef-client 后台静默监听
reef 独占:  xiaozhi-ai 暂停 (vTaskSuspend), ePaper 全屏归 reef 使用
```

```c
typedef enum { MODE_NORMAL, MODE_REEF_EXCLUSIVE } device_mode_t;
static device_mode_t g_mode = MODE_NORMAL;

static void mode_btn_task(void *arg) {
    int hold_ms = 0;
    while (1) {
        vTaskDelay(pdMS_TO_TICKS(100));
        if (gpio_get_level(BTN_PIN) == 0) {
            hold_ms += 100;
            if (hold_ms >= 3000) {
                g_mode = (g_mode == MODE_NORMAL) ? MODE_REEF_EXCLUSIVE : MODE_NORMAL;
                if (g_mode == MODE_REEF_EXCLUSIVE) {
                    vTaskSuspend(xiaozhi_task_handle);
                    epaper_clear();
                    epaper_draw_text(50, 90, "REEF MODE", FONT_24);
                    epaper_refresh();
                } else {
                    vTaskResume(xiaozhi_task_handle);
                }
                hold_ms = 0;
            }
        } else {
            hold_ms = 0;
        }
    }
}
```

---

## 5. 配置存储 (NVS Schema)

```
Namespace: "reef"
+--------------------------+-----------+---------------------------+
| Key                      | Type      | Default                   |
+--------------------------+-----------+---------------------------+
| server_url               | string    | "ws://192.168.1.100:9999/ws" |
| token                    | string    | ""                        |
| client_id                | string    | "esp32-epaper-01"         |
| role                     | string    | "edge-device"             |
| mqtt_broker (可选)        | string    | ""                        |
| enable_display_text      | u8 (bool) | 1                         |
| enable_read_battery      | u8 (bool) | 1                         |
| enable_ota               | u8 (bool) | 0                         |
| heartbeat_interval_ms    | u32       | 30000                     |
+--------------------------+-----------+---------------------------+
```

```c
// 读取
nvs_handle_t nvs;
nvs_open("reef", NVS_READONLY, &nvs);
size_t len = 128;
char buf[128];
nvs_get_str(nvs, "server_url", buf, &len);
nvs_close(nvs);
```

---

## 6. OTA 与升级

### 6.1 方案概述

reef-server 通过 `task_dispatch` 下发 OTA 指令：

```json
{
  "msg_type": "task_dispatch",
  "task_id": "ota-xxxx",
  "payload": {
    "instruction": "ota_update:https://reef-server/releases/esp32-s3-reef-v1.2.3.bin",
    "parameters": { "sha256": "abc123...", "version": "1.2.3", "size": 524288 }
  }
}
```

ESP32 执行：

1. 收到 `ota_update:<url>` 指令
2. 通过 `esp_http_client` 下载固件到 OTA 分区
3. SHA256 校验
4. 调用 `esp_ota_end()` + `esp_restart()`
5. 新固件启动后重新连接 reef-server，发送 register（`client_id` 不变）

### 6.2 关键约束

- ESP32-S3 需要至少 2 个 OTA app partition（factory + ota_0）
- 单次 OTA 下载约占用 200-300 KB heap
- reef-client 模块与 xiaozhi-ai 共享同一固件 image，升级时两者一起更新

---

## 7. 资源预算

### 7.1 Flash 占用估算

| 模块 | 大小 |
|------|------|
| `esp_websocket_client` | ~8 KB |
| `cJSON` | ~4 KB |
| reef-client 核心（connect + register + heartbeat + dispatch） | ~6 KB |
| skill handlers（display_text + battery + led + audio） | ~3 KB |
| NVS 配置 | ~2 KB |
| OTA 支持 | ~2 KB |
| **reef-client 总计** | **~25 KB** |

> 对于 ESP32-S3 的 16 MB Flash，reef-client 固件增量 < 25 KB，小于 0.2%。

### 7.2 RAM 占用

| 组件 | 大小 |
|------|------|
| WebSocket rx/tx buffer | 8 KB |
| cJSON 解析临时分配 | 2-4 KB |
| Skill result buffer | 1 KB |
| FreeRTOS task stacks（reef_ws + reef_exec + mode_btn） | 6+6+2 = 14 KB |
| **reef 运行时 RAM** | **~28 KB** |

### 7.3 可用资源分析

ESP32-S3 典型配置：
- **IRAM**: 512 KB（~300 KB free after xiaozhi-ai）
- **DRAM**: 512 KB（~200 KB free after xiaozhi-ai + Wi-Fi stack）
- **PSRAM**: 8 MB（充裕，xiaozhi-ai 占用 ~1.5 MB）

> **结论**: reef-client 在资源上完全可行。14 KB task stack + 8 KB buffer + 6 KB heap 峰值，总计 ~30 KB DRAM。xiaozhi-ai 剩余 200 KB DRAM 绰绰有余。PSRAM 更不必担心。

---

## 附录：关键代码骨架（完整）

### message.c — JSON 编解码

```c
// reef_msg_send(msg_type, task_id, payload_json)
void reef_send_task_completed(const char *task_id, const char *result_text) {
    cJSON *root = cJSON_CreateObject();
    cJSON_AddStringToObject(root, "msg_type", "task_completed");
    cJSON_AddStringToObject(root, "task_id", task_id);
    cJSON_AddNumberToObject(root, "timestamp", (double)time(NULL) * 1000);

    cJSON *payload = cJSON_CreateObject();
    cJSON_AddStringToObject(payload, "text", result_text);
    cJSON_AddItemToObject(root, "payload", payload);

    char *json_str = cJSON_PrintUnformatted(root);
    esp_websocket_client_send_text(ws_client, json_str, strlen(json_str), portMAX_DELAY);
    cJSON_free(json_str);
    cJSON_Delete(root);
}

void reef_send_task_failed(const char *task_id, const char *err_type, const char *err_msg) {
    cJSON *root = cJSON_CreateObject();
    cJSON_AddStringToObject(root, "msg_type", "task_failed");
    cJSON_AddStringToObject(root, "task_id", task_id);
    cJSON_AddNumberToObject(root, "timestamp", (double)time(NULL) * 1000);

    cJSON *payload = cJSON_CreateObject();
    cJSON_AddStringToObject(payload, "error_type", err_type);
    cJSON_AddStringToObject(payload, "error_message", err_msg);
    cJSON_AddItemToObject(root, "payload", payload);

    char *json_str = cJSON_PrintUnformatted(root);
    esp_websocket_client_send_text(ws_client, json_str, strlen(json_str), portMAX_DELAY);
    cJSON_free(json_str);
    cJSON_Delete(root);
}
```

### main.c — 入口 & 初始化

```c
void app_main(void) {
    // --- 公共初始化 ---
    nvs_flash_init();
    wifi_connect();           // 与 xiaozhi-ai 共用 Wi-Fi
    epaper_init();
    epaper_mutex = xSemaphoreCreateMutex();

    // --- xiaozhi-ai 启动（已有） ---
    xTaskCreate(xiaozhi_main_task, "xiaozhi", 8192, NULL, 5,
                &xiaozhi_task_handle);

    // --- reef-client 启动 ---
    xTaskCreate(reef_heartbeat_task,  "reef_hb",   4096, NULL, 2, NULL);
    xTaskCreate(reef_executor_task,   "reef_exec", 6144, NULL, 3, NULL);
    xTaskCreate(mode_btn_task,        "mode_btn",  2048, NULL, 2, NULL);

    // --- reef WS 连接（在主 task 上下文，阻塞式） ---
    reef_ws_connect();
    reef_send_register();
    // reef_ws_event_handler 会在 WEBSOCKET_EVENT_DATA 中调用
    // reef_on_message() 进行 JSON 解析和任务分发
}
```

---

**总结**：reef-client 作为 xiaozhi-ai 的并行模块，通过独立的 FreeRTOS task 维护 WebSocket 长连接、处理 server 下发的 hardware skill 任务、上报结果。资源开销控制在 Flash 25 KB / RAM 30 KB 以内，对 xiaozhi-ai 主业务无影响，两者通过 FreeRTOS 信号量协调 ePaper 访问。
