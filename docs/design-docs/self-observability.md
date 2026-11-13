---
title: 프락시움 자신의 관측성과 운영 가능성 — 신호, 진단, 운영 행위, 권한과 기록
kind: guide
status: active
follows:
  - docs/design-docs/adr/0017-self-observability-and-operability.md
---
# 프락시움 자신의 관측성과 운영 가능성

제품 책임자의 요구는 이래요. 프락시움 백엔드 자체에 대한 관측성도 확보돼야 하고, 나중에 AI 에이전트가 프락시움도 자율 운영할 수 있어야 해요. 이 문서는 프락시움이라는 시스템이 잘 돌고 있는지 보고, 아프면 까닭을 가리고, 정해진 행위로 돌보고, 그 기록을 남기는 설계예요. 소스에서 들어온 데이터가 믿을 만한지는 [데이터 관측성](data-observability.md)(ADR-0013)이 맡아요. 숫자는 기본값(설정)이고 [기본값 표](defaults.md)와 14절에 있어요.

## 1. 무엇에 답하나
| 쟁점 | 답 |
|---|---|
| 프락시움이 잘 돌고 있나, 무엇이 왜 아픈가 | 역할 넷의 신호, 스스로 재기, 건강과 `diagnose`, 사건 코드의 목록(3~6절) |
| 에이전트는 무엇을 해도 되나, 한 일이 남나 | 운영 행위의 목록, 위험의 축, 승인 필요, 설정, 감사 기록(7~9절) |
| 프락시움이 아플 때 프락시움에 물을 수 있나 | 저장소 없이 답하는 범위, 지켜보는 것을 지켜보기(5, 12절) |
| 에이전트는 어느 문으로 오나 | 운영의 MCP 면(10절) |

## 2. 한눈에
에이전트가 운영하려면 넷이 있어야 해요. 지표를 내는 것으로는 모자라요.
```
 ① 신호                  ② 진단                  ③ 운영 행위                   ④ 권한과 기록
 역할 넷의 지표·로그·추적 ─▶ `diagnose`           ─▶ 목록의 행위 ─▶ 미리 해 보기 ─▶ 역할·범위·승인 필요 ─▶ 실행
 스스로 재기(합성 점검)     규칙이 맞은 것과 근거     위험의 축 넷, 선언한 효과       감사 기록(어떤 진단을 근거로)
       ▲                    사건 코드의 목록(계약)                                        │
       └─────────── 뒤의 확인: 선언한 효과가 신호에 나타났나 ◀────────────────────────────┘
 (밖의 에이전트가 MCP 게이트웨이를 거쳐 ②③을 불러요. 프락시움 안에는 스스로 고치는 루프가 없어요)
```
**ADR-0013과의 나눔.** ADR-0013은 데이터(소스, 데이터셋, 공급, 타입, 속성의 건강과 정의의 계보를 따른 까닭)를, 이 문서는 시스템(`component`: 역할의 벌, 기대는 것, 줄, 투영의 건강과 부품의 기댐을 따른 까닭)을 봐요. 지표 표 `metric_point`, 건강 여섯 상태, 알림, 문제 목록은 같은 틀이에요. 둘은 데이터 쪽의 증상(밀림, 묵음)과 그 시스템 쪽의 까닭(일꾼이 죽음, 연결이 참)에서 만나요.

## 3. 신호
### 3.1 틀
- 요청은 수·오류·시간(RED)으로, 자원은 쓰임·포화·오류(USE)로 재요. 표의 「방법」 칸이 R(수), E(오류), D(시간), U(쓰임), S(포화)예요. 지표는 `metric_point`에 `resource_kind: component`로 쌓이고 `resource_id`는 벌의 `instance_id`나 기대는 것의 이름, `detail_key`는 닫힌 목록이에요.
- 이름은 영어 snake_case예요. ADR-0013 13절에 있는 이름(「13절」)은 그대로 써요. 열린 형식으로 낼 때 의미 관례가 있는 것(요청의 수·시간)은 그 이름으로 바꿔 내요. 어느 영역의 관례가 안정인지는 구현할 때 확인해요.

### 3.2 역할마다 내는 지표
| 역할 | 지표 | 뜻 | 단위 | 방법 |
|---|---|---|---|---|
| 모든 역할 | `instance_up`, `process_restarts`, `process_cpu_ratio`, `process_memory_bytes` | 벌이 떠 있나, 다시 뜬 횟수, CPU와 메모리의 쓰임 | 0/1, 번, 비율, 바이트 | E, U |
| | `db_pool_in_use`, `db_pool_waiting`, `db_pool_max` | 저장소로 가는 연결 풀의 쓰임, 기다리는 수, 크기 | 개 | U, S |
| | `dependency_up`, `dependency_call_errors`, `dependency_call_duration_ms` | 기대는 것(`detail_key`: `storage`, `change_log`, `file_store`, `sso`, `notifier`, `secret_provider`, `master_key`, `engine`, `api`)에 닿나(0/1/모름), 호출 오류, 호출 시간 | 0/1, 건, ms | E, D |
| | `role_version_info` | 역할, 코드 버전, 사건 코드 목록 버전, 계약 버전(값은 1, 정보는 이름표) | 1 | |
| API | `api_requests`, `api_request_errors`, `api_request_duration_ms`, `api_inflight_requests` | 길의 무리(`detail_key`)마다 요청 수, 오류(오류 `code`의 무리), 응답 시간, 진행 중인 요청 | 건, ms | R, E, D, U |
| | `api_error_ratio`(13절), `api_rate_limited`, `grant_cache_age_s` | 오류의 비율, 한도에 걸린 호출(`detail_key`: `requests`, `concurrency`, `compute_budget`), 권한 사본의 나이(상한이 있어요) | 비율, 건, 초 | E, S |
| 일꾼 | `worker_alive`, `lease_takeovers`(13절), `stale_attempt_rejections` | 살았나, 죽은 일을 이어받은 수, 번호(`attempt`)로 막힌 늦은 쓰기 | 0/1, 건 | E |
| | `queue_depth`, `oldest_job_age_s`(13절), `tick_lag_s` | 줄에 선 일 수와 가장 오래 기다린 일(`detail_key`는 일의 종류. 예: `sync`, `stream_apply`, `candidate_run`, `merge`, `retention`, `synthetic_probe`), 일정 시계의 늦음 | 건, 초 | S |
| | `jobs_started`, `job_failures`, `job_duration_s`, `apply_duration_s`, `candidate_run_duration_s` | 일의 수, 실패(`detail_key`가 오류 코드), 시간, 적용 한 번과 후보 실행의 시간 | 건, 초 | R, E, D |
| 질의 엔진 | `engine_queries`, `engine_query_errors`, `engine_query_duration_ms` | 질의 이름(`detail_key`)마다 수, 오류, 응답 시간 | 건, ms | R, E, D |
| | `engine_memory_bytes`, `engine_loaded_version_age_s`, `projection_lag_seq`(13절), `engine_overlay_size`, `engine_compaction_duration_s`, `engine_swap_duration_s` | 메모리, 올려 둔 버전의 나이, 반영하지 못한 `change_seq`의 수, 덧붙임 겹의 크기, 다지기와 갈아 끼움의 시간 | 바이트, 초, 건 | U, S, D |
| 수집기 | `collector_alive`, `collector_last_seen_age_s`(3절 데이터 쪽), `collector_poll_errors`, `collector_poll_duration_s`, `collector_envelopes_sent` | 살았나(본체에 일을 물으러 온 때가 곧 생존 신호), 소스 읽기의 오류와 시간, 보낸 봉투 | 0/1, 초, 건 | R, E, D |
| | `collector_outbox_depth`, `collector_outbox_oldest_age_s` | 보내지 못해 쌓인 봉투와 가장 오래된 것 | 건, 초 | S |

수집기와 엔진은 자기 지표를 열린 형식으로 직접 내요(ADR-0011). 일꾼이 거두지 못한 구간은 0이 아니라 비어(모름) 남아요.

### 3.3 프락시움에 고유한 지표
소스에서 엔진까지는 구간 셋이 이어져 있어요. 밀림이 어느 구간에 있는지가 까닭을 가르는 첫 단서예요.
```
 소스 ─[수집의 밀림]─▶ 받은 변경의 기록 ─[적용의 밀림]─▶ KG(`applied_seq`) ─[투영의 밀림]─▶ 질의 엔진
```
| 지표 | 뜻 | 단위 |
|---|---|---|
| `ingest_lag_s` | 수집의 밀림. 소스가 끝을 알려 주면 그 끝과 기록에 쓴 자리의 차, 아니면 마지막으로 기록에 쓴 때의 나이(`detail_key`: `cursor_gap`, `last_write_age`) | 초 |
| `apply_lag_seq`, `apply_lag_s` | 적용의 밀림. 받은 변경의 기록의 맨 끝과 `applied_through`의 차(`ingest_seq`의 수), 적용 못 한 가장 오래된 변경의 나이 | 건, 초 |
| `projection_lag_seq`, `projection_lag_s` | 투영의 밀림. KG의 `applied_seq`(큰 적용은 읽는 자리 `readable_through`)와 엔진이 반영한 `change_seq`의 차와 그 나이. 엔진은 `volatile` 값의 가벼운 변경의 줄도 함께 따르고 그 줄의 밀림도 같은 지표로 내요 | 건, 초 |
| `held_streams`, `held_oldest_age_s`, `held_backlog_ratio` | 붙잡힌 흐름의 수, 가장 오래된 것의 나이, 쌓인 양 ÷ 보관의 한도(ADR-0011) | 개, 초, 비율 |
| `merge_queue_depth`, `merge_oldest_wait_s` | 병합의 줄의 길이와 가장 오래 기다린 제안 | 건, 초 |
| `storage_bytes`, `storage_growth_bytes_per_day`, `storage_connections_in_use`, `storage_connections_max` | 저장소의 크기(`detail_key`: `live`, `hist`, `raw_record`, `change_log`, `metric`, `audit`, `other`), 늘어나는 빠르기, 저장소가 본 연결 수와 한도 | 바이트, 개 |
| `version_skew_age_s`, `time_to_limit_s` | 역할 사이의 버전이 어긋난 채 지난 시간(한 가지면 0), 이대로면 한도에 닿기까지(12.3) | 초 |
| `secret_master_key_reachable`, `secret_expires_in_s` | 비밀 값(ADR-0018): 으뜸 열쇠에 닿나(비밀 값을 푸는 일꾼의 수집 부분과 수집기만 내요. API와 엔진은 풀 수 없어 없는 게 정상이에요), 비밀 값의 만료까지(`detail_key`는 비밀 값의 `id`이고 값이 아니에요) | 0/1/모름, 초 |
| `secret_unwrap_count`, `secret_scrubbed_fields`, `tls_verify_disabled_sources`, `at_rest_encryption_enabled` | 풀어 쓴 수(`detail_key`: 역할), 받을 때 지운 비밀 값의 수, 인증서 확인을 끈 소스의 수, 저장소의 쉬는 데이터 암호화가 켜져 있나(점검) | 건, 개, 0/1/모름 |
| `storage_dead_tuple_ratio`, `storage_xid_age`, `storage_wal_bytes_per_s`, `storage_replication_lag_s`, `storage_long_txn_age_s`, `storage_slot_retained_bytes`, `storage_retention_lag_s`, `storage_migration_lock_wait_s` | 저장소의 신호(8-26): 죽은 행과 팽창, 트랜잭션 번호의 나이, WAL의 양, 복제 지연, 긴 트랜잭션, 슬롯이 붙잡은 양, 보관의 밀림, 옮김의 잠금 대기. 사건 코드는 `storage.*`예요. 구현은 단계별(13절)이고 이름과 단위는 알파부터 계약이에요 | 비율, 번호, 바이트, 초 |
| `message_log_reachable`, `message_log_lag`, `message_log_write_errors`, `message_log_retention_margin_s` | 붙은 메시지 기록 저장소(클로즈 베타. 제품이 띄우지 않아요): 닿음, 밀림, 쓰기 실패, 보관까지 남은 여유 | 0/1/모름, 건, 초 |
| `ops_action_runs`, `ops_action_verdicts` | 운영 행위의 실행(`detail_key`: 행위 이름과 결과)과 뒤의 확인의 판정 | 건 |

### 3.4 로그 한 줄과 추적
한 줄은 JSON 한 개예요. 칸: `ts`(UTC), `severity`(`debug` / `info` / `warning` / `error` / `critical`), `event_code`(안정된 사건 코드. 6절 목록에 있어야 해요), `trace_id`와 `span_id`(오류 응답의 `trace_id`와 같아요), `role`과 `instance_id`, `resource_kind`와 `resource_id`(일의 자원. 객체나 행이 아니에요), `params`(매개 값. 키는 코드마다 목록에 닫혀 있고 값은 ID, 수, 고르기 값, 시간뿐이에요), `message`(참고용 영어 한 줄. 기계는 읽지 않아요). 같은 코드가 폭주하면 표본으로 줄이고 버린 수를 `log_dropped`로 세요. 비밀 값과 행의 내용은 어느 칸에도 없고, 시험이 "찍는 모든 칸이 목록에 있나"를 걸어요.
```json
{"ts":"2031-04-02T07:41:12.304Z","severity":"warning","event_code":"storage.connections_exhausted","trace_id":"tr_7c1e","span_id":"sp_02","role":"worker","instance_id":"wk-2","resource_kind":"component","resource_id":"storage","params":{"limit_at":"pool","pool":"worker","in_use":16,"max":16,"waiting":9},"message":"worker storage pool exhausted"}
```
추적은 요청 하나, 동기화 한 번이 하나예요. 줄을 거치는 곳은 부모와 자식이 아니라 링크로 이어요. 작업 기록과 실행 기록에 `trace_id`를 남겨서 줄에서 꺼낸 쪽이 이어 붙여요. 이름표는 역할, 자원의 `id`, 사건 코드, 수뿐이에요. 오류와 느린 요청은 모두, 나머지는 표본으로 내요. 추적과 로그는 제품 안에 쌓지 않고 열린 형식으로 내요(보관은 받는 쪽의 일이에요). 실행마다의 단계별 수와 시간은 제품이 저장해요(ADR-0013).
```
 요청:   api 길 ─▶ 권한 ─▶ (읽기) engine 질의 ─▶ 저장소(있으면) ;  (쓰기, 오래 걸림) 작업 만들기 ──링크──▶ worker 작업 ─▶ 단계 ─▶ 저장소
 동기화: worker `sync_run`(뿌리) ─▶ 수집 ─▶ 정제 ─▶ 매핑 ─▶ 식별 ─▶ 링크 ─▶ 차이 ─▶ 안전장치 ─▶ 적용 ──링크(`change_seq`)──▶ engine 투영 반영
```

### 3.5 프락시움 자신의 목표
ADR-0013 6절의 틀(`threshold`, `period`, `allowed_miss`, 못 지킨 비율, 빠르게 나빠짐)을 그대로 써요. 목표도 정의의 항목이라 버전, 제안, 승인을 타요. `target`이 `component`이고 `kind`는 `latency`, `lag`, `on_time_ratio`, `error_ratio`예요(ADR-0013 6절의 닫힌 목록에 더하는 넷).

| 목표 | 대상 | 기본값(설정) |
|---|---|---|
| 물음의 응답 시간 | 엔진, API의 질의 길 | `impact`는 제품 책임자의 숫자(REQ-142), 그 밖의 닫힌 질의는 p95 1초 |
| 적용의 밀림, 투영의 밀림 | 일꾼, 엔진 | 흐름의 `apply_lag_s` 60초, `projection_lag_s` 30초 이내(배치는 다음 주기 전) |
| 일정의 제때 실행, 스스로 재기 | 일꾼, 합성 점검 | `tick_lag_s` 10초 이내 99%, `total` 60초 이내 99% |

## 4. 끝에서 끝까지 스스로 재기
부품의 지표가 다 초록이어도 사용자가 보는 답이 막힐 수 있어요. 그래서 제품이 지어낸 변경을 스스로 흘려 사용자와 같은 길로 답이 나타나는지 재요(합성 점검, 흑상자 점검). 부품의 지표는 까닭을, 이것은 증상을 말해요.
- **지어낸 소스.** 제품에 든 소스 `synthetic_probe`예요(`sys` 타입을 시스템 KG에 대요). 읽기만 하는 어댑터가 정해진 간격(흐름 60초, 배치 15분)으로 지어낸 기록을 내요. 어디에도 쓰지 않아요.
- **시스템 KG.** 객체는 운영의 KG가 아닌 따로 된 시스템 KG에 담기고, 타입은 예약된 이름 공간 `sys`의 것(`system` 층)이에요. 이 KG는 `sys` 항목만 쓰는 정의 묶음을 가리켜요(11절). 서버 하나, 전원 장치 둘, 이중화 묶음 하나(`min_alive` 1), 표지 객체 하나(속성 `probe_seq`)뿐이고 표지의 속성만 올려서 객체 수가 늘지 않아요. 운영 KG와 `impact`, 건강, 역량 질문에 섞이지 않아요. 길은 `/v1/kgs/{kg}/...`이고 점검의 시스템 계정은 그 KG만 읽어요.
- **재는 것.** 같은 변경을 네 걸음에서 확인해요. 마지막 걸음은 시스템 계정(`acct_self_probe`, 그 KG만 `viewer`)이 보통의 HTTP 요청을 보내서 로그인, 한도, 봉투, 엔진까지 한 번에 지나가요. 덧붙여 정해진 죽은 객체(전원 장치 하나)로 `impact`를 물어 기대한 답(서버 `exposed_direct`)과 견줘요.
- **지표.** `e2e_probe_latency_s`(`detail_key`: `emit_to_ingested` 받은 변경의 기록에 적힘, `ingested_to_applied` `applied_through`가 넘음, `applied_to_projected` 엔진이 `change_seq`를 반영, `projected_to_answered` API의 답에 나타남, `total`), `e2e_probe_success`(제한 시간 안에 나타났나), `e2e_probe_age_s`(마지막 성공의 나이), `e2e_probe_answer_correct`.

**실패했을 때 무엇을 아나.** 막힌 걸음이 까닭의 첫 단서예요.

| 막힌 곳 | 의심 | 이어지는 진단 |
|---|---|---|
| 수집(첫째 걸음) | 수집기, 받은 변경의 기록 | `ingest.*`, `change_log.*` |
| 적용(둘째) | 일꾼, 저장소, 붙잡힘 | `apply.*`, `storage.*` |
| 투영(셋째) | 엔진, 투영으로 가는 변경의 길, 파일 보관 | `engine.projection_lagging`, `file_store.*` |
| 답(넷째), 답이 틀림 | API, 권한 사본, 엔진 호출 / 영향 범위의 계산이나 투영의 반영 | `api.*`, `auth.*` / `engine.answer_mismatch` |
| 결과가 안 옴 | 점검을 돌리는 일꾼 자체 | `e2e_probe_age_s`를 밖에서 봐요(12절) |

한계: 점검은 일꾼의 일정으로 돌아서 일꾼이 다 멎으면 같이 멎어요. 그래서 결과가 안 오는 것이 곧 실패 신호예요. 작은 그래프 하나만 재므로 큰 KG의 성능은 못 재고, 소스 어댑터는 지나지 않아요(소스 쪽 점검이 맡아요).

## 5. 건강과 진단
### 5.1 역할마다 셋과 기대는 것
널리 쓰는 건강 점검처럼 셋을 갈라요. **떠 있나**는 기대는 것을 보지 않아요(한 곳의 고장이 모든 벌을 죽이지 않게). **받을 수 있나**는 그 역할의 본업 하나를 기준으로 하고, **다 올라왔나**가 참이 되기 전에는 거짓이에요. 답은 낮은 비용이고 로그인이 없고 내용이 없어요. 길은 `GET /v1/health/live`, `GET /v1/health/ready`, `GET /v1/health/started` 셋이에요(자원의 건강 목록 `/v1/health`와 다른 것이고 배포의 점검이 써요). 받을 수 있나의 답은 `{ready, can: {ask, write, ingest, apply}, reasons: [사건 코드]}`예요. 길(HTTP 상태)은 본업만 따라요.

| 역할 | 떠 있나(`live`) | 받을 수 있나(`ready`, 본업) | 다 올라왔나(`started`) |
|---|---|---|---|
| API | 프로세스가 답해요 | **묻기**를 받을 수 있나: 권한 사본이 있고 엔진이나 SQL 길 중 하나가 돼요. 저장소가 멈춰도 참이에요(`can.write`만 거짓) | 설정 사본과 권한 사본을 처음 읽었어요 |
| 일꾼 | 프로세스가 답하고 점유를 연장하는 고리가 돌아요 | 저장소에 쓸 수 있고 일을 쥘 수 있어요 | 스키마 버전이 코드의 기대와 맞고 일꾼으로 등록됐어요 |
| 질의 엔진 | 프로세스가 답해요 | 투영이 하나라도 올라 있어요(저장소와 무관) | 처음 투영을 올렸어요 |
| 수집기 | 프로세스가 답해요 | 본체에 닿고 일을 받아 소스를 읽을 수 있어요 | 허용된 소스와 읽은 자리를 받았어요 |

기대는 것(저장소, 받은 변경의 기록, 파일 보관, SSO, 알림의 길, 비밀 값 제공자)은 역할이 호출한 결과(`dependency_up`)와 가벼운 점검으로 건강을 따로 내요. 부품도 ADR-0013 5절의 여섯 상태와 규칙을 써요. 기댐은 제품에 실려 있어요: API는 엔진(묻기)에 hard, 엔진은 파일 보관(투영 파일)과 변경의 이어짐에 hard, API는 저장소(쓰기와 사본 새로 읽기)와 SSO(증표 확인)에 soft(묻기에는), 일꾼의 수집 부분은 으뜸 열쇠(`master_key`)에도 hard, 일꾼은 저장소, 받은 변경의 기록, 파일 보관에 hard이고 비밀 값 제공자, 알림의 길, 소스에 soft, 수집기는 본체에 hard예요.

### 5.2 까닭이 올라가는 예시
지어낸 예시예요. 까닭은 부품의 기댐을 따라 위로 올라가요.
```
 물음의 응답 시간 목표가 빠르게 나빠짐     engine    degraded           (자기 검사: 덧붙임 겹이 커져 다지기가 겹침)
   ◀ engine.projection_lagging             engine    degraded           투영이 12분 뒤처져 변경이 한꺼번에 몰려 들어옴
     ◀ apply.lag_high                      worker    affected_upstream  적용이 밀려 투영으로 갈 변경이 늦게 옴
       ◀ storage.connections_exhausted     storage   degraded           ◀ 뿌리 까닭: 일꾼의 풀 16/16, 기다림 9
```
뿌리 까닭 하나에 딸린 알림은 하나로 묶여요(ADR-0013 8.2). 응답이 느린 까닭이 밀린 변경을 몰아 따라잡는 비용이라는 설명은 재서 확인할 가정이에요.

### 5.3 `diagnose`
`POST /v1/ops/diagnose`예요. 상태를 바꾸지 않아요(안전함).
- **입력.** `target`(부품 목록, 역할, 또는 전체), `symptom_ref`(알림 `alert`, 목표 `objective`, 사건 코드 `event_code` 중 하나. 없으면 지금 아픈 것 전부), `include`(`evidence`, `actions`), `depth`(기댐을 몇 걸음까지 올라갈지), `limit`, `cursor`.

| 출력 | 뜻 |
|---|---|
| `diagnosis_id`, `evaluated_at`, `catalog_version` | 진단의 이름표, 때, 쓴 규칙 묶음의 버전 |
| `served_by`, `persisted` | 어느 벌이 답했나, 저장소에도 남겼나(못 남기면 그 벌의 메모리에 `ops.diagnosis_ttl_s` 동안만 있어요) |
| `coverage` | 보고한 벌과 못 한 벌. **보고하지 못한 것은 건강한 것이 아니라 모름이에요** |
| `findings[]` | 아픈 것 하나마다: `component`, `state`, `symptom`(무엇이 아픈가) |
| `findings[].suspected_causes[]` | 까닭으로 보이는 것: `code`, `component`, `via`(기댐의 길), `root`, `rule_matched`(`rule_id`, `catalog_version`) |
| `findings[].evidence[]` | 근거가 된 신호: `signal`, `resource`, `observed`, `threshold`, `window`, `at` |
| `findings[].actions[]` | 할 수 있는 운영 행위: `action`, `params`, `reason_rule`, `requires_approval`(귀띔), `dry_run`(길) |
| `unknowns[]` | 판정할 신호가 없어 모르는 것 |

**확신의 정도는 없어요.** 어느 규칙이 맞았는지(`rule_matched`)만 있어요. 맞는 규칙이 없으면 `suspected_causes`가 비고 `unknown_cause: true`예요(지어내지 않아요). 규칙은 6절의 사건 코드 항목(`common_causes`, `look_at`, `actions`)에서 나온 정해진 조건이고, 같은 신호면 같은 진단이에요.

### 5.4 저장소 없이 답하는 범위
프락시움이 아플 때 프락시움에 물어야 해서, 기본은 역할이 메모리에 쥔 것으로 답해요(ADR-0011).

| 저장소 없이 되는 것 | 저장소가 있어야 하는 것 |
|---|---|
| `live`, `ready`, `started`, 역할이 쥔 최근 신호(링 버퍼. 길이는 14절)와 기대는 것의 건강 | 지난 흐름(`metric_point`), 시간줄, 알림 목록 |
| `diagnose`(`persisted: false`), 사건 코드의 목록(제품에 실려 있어요), 행위의 목록 | 감사 기록과 실행 기록을 남기는 일, 설정을 바꾸는 일(마지막 사본은 읽혀요) |
| 저장소를 안 쓰는 미리 해 보기, 급한 행위 셋(아래) | 나머지 운영 행위의 실행(감사를 못 남겨서요) |

- **신호를 모으는 길.** 역할마다 자기 건강과 지표를 내는 길이 있고, API 역할이 다른 벌에 직접 물어 모아요(벌의 목록은 생존 신호로 메모리에 쥐어요). 닿지 못한 벌은 답에 「모름」으로 적혀요.
- **감사 기록을 못 쓸 때.** 바꾸는 운영 행위는 거절해요(기록 없이 한 행위는 없어요). 예외는 저장소 없이도 되는 급한 행위 셋(`pause_source`, `pause_sources_by_secret`, `enter_maintenance_mode`)이에요. 이것은 역할의 디스크에 먼저 적어 두었다가 저장소가 돌아오면 감사 기록으로 옮겨요. 디스크에도 못 적으면 거절해요.
- **엔진이 새로 뜰 때.** 올릴 투영이 파일 보관에 없으면 저장소에서 직접 지어 올려요. 파일 보관은 빨리 뜨기 위한 것이지 없으면 안 되는 것이 아니에요. 저장소도 없고 파일도 없으면 `started`가 거짓으로 남아요.
- **긴 장애에서 권한 사본의 나이.** 저장소 없이 답하는 동안 권한 사본이 나이 상한(15분)을 넘으면 기본 부여의 읽기만 답해요(`restricted` 이상은 거절). 그래서 장애가 길면 묻기가 줄어요. `grant_cache_age_s`가 상한에 다가가면 알려요.

## 6. 사건 코드와 오류 코드의 목록
로그의 사건 코드, API의 오류 코드, 알림의 종류, 건강의 까닭을 **하나의 목록**으로 실어요. 기계가 읽는 파일이고 버전이 있고 **계약의 일부**예요. 사람용 절차서의 글은 이 목록에서 만들고 손으로 따로 적지 않아요.

**이름 규칙.** `<무리>.<세부>`, 소문자 snake_case예요. 요청 오류의 무리는 REST의 것(`invalid_input`, `not_found`, `permission`, `conflict`, `stale`, `rate_limited`, `blocked`)을 그대로 써요. 시스템의 무리는 `api`, `auth`, `worker`, `engine`, `collector`, `storage`, `change_log`, `file_store`, `sso`, `notify`, `ingest`, `apply`, `probe`, `config`, `rollout`, `secret`, `ops`예요. 비밀 값에 걸린 코드(`secret.master_key_unreachable`, `secret.expiring`, `secret.unwrap_failed`)도 같은 목록에 있어요(ADR-0018). 한번 낸 코드의 뜻은 바꾸지 않고, 더하기는 같은 큰 버전 안에서 되고, 지울 때는 `deprecated`로 남겨요.

| 칸 | 뜻 |
|---|---|
| `code`, `kinds` | 이름, 쓰이는 곳(`event` 로그 / `error` API 오류 / `alert` 알림의 종류 / `cause` 건강의 까닭. 여럿일 수 있어요) |
| `severity`, `alert_severity`, `meaning`, `params` | 로그의 심각도, 알림이 되면 `info` / `warning` / `critical`, 뜻 한 줄(영어. 화면의 글은 코드로 만들어요), 매개 값의 키와 형 |
| `common_causes`, `look_at` | 흔한 까닭(다른 `cause` 코드), 살펴볼 것(신호, 자원, 기대하는 모양) |
| `actions`, `since`, `status` | 할 수 있는 운영 행위(이름, 때, 되돌리는 행위), 처음 낸 목록 버전, `active` / `deprecated` |

**버전과 길.** 목록의 버전은 `catalog_version`이에요(코드를 더하면 작은 번호, 칸의 모양을 바꾸면 큰 번호). 역할은 자기가 쓰는 버전을 `role_version_info`로 내요. `GET /v1/ops/event-codes`가 목록을, `GET /v1/problems/{code}`가 오류 코드의 설명을 줘요(`storage.*`와 `secret.version_check_failed`도 이 목록에 있어요). 시험이 셋을 걸어요: 찍는 모든 코드가 목록에 있고, `actions`가 7절의 목록 안에 있고, `look_at`의 지표가 지표 목록에 있어요.

지어낸 항목 넷이에요. 첫째는 파일의 모양 그대로, 나머지는 같은 칸을 표로 적었어요.
```yaml
- code: storage.connections_exhausted
  kinds: [event, cause, alert]
  severity: warning
  alert_severity: critical
  meaning: connections to the storage are used up
  params: {limit_at: "pool|server", pool: "api|worker|engine", in_use: int, max: int, waiting: int}
  common_causes: [worker.candidate_runs_hold_connections, worker.pool_oversized]
  look_at: [{signal: db_pool_in_use, resource: worker, want: "max와 같다"}, {signal: db_pool_waiting, resource: worker, want: "0보다 크다"}]
  actions: [{action: update_setting, when: "limit_at == pool", hint: "worker.candidate_run_concurrency를 줄인다", reverse: revert_setting}, {action: drain_worker, when: "한 벌이 대부분을 쥠", reverse: undrain_worker}]
```
| 코드, 종류, 심각도 | 매개 값 | 흔한 까닭 | 살펴볼 것 | 할 수 있는 운영 행위 |
|---|---|---|---|---|
| `apply.lag_high` · `cause`, `alert` · warning, 알림 warning | `lag_seq`, `lag_s` | `storage.connections_exhausted`, `worker.all_drained`, `ingest.stream_held` | `apply_lag_seq`(늘어남), `oldest_job_age_s`(`stream_apply`, 김), `held_streams`(0이면 붙잡힘 아님) | `release_held_stream`(`held_streams` > 0일 때), `undrain_worker`(모든 벌이 비워졌을 때) |
| `engine.projection_lagging` · `event`, `cause` · warning | `lag_seq`, `lag_s`, `instance` | `apply.lag_high`, `file_store.unreachable`, `engine.compaction_slow` | `projection_lag_seq`(늘어남), `engine_compaction_duration_s`(김) | `rebuild_projection`(1800초 넘게 뒤처지고 상류가 멀쩡할 때), `roll_engine`(한 벌만 뒤처질 때) |
| `stale.dry_run_outdated` · `error` · info | `dry_run_id`, `changed`(`target`, `setting_rev`, `risk`) | 미리 해 본 뒤 대상이 바뀜 | `ops_dry_run_action`의 `expires_at` | 미리 해 보기를 다시 해요 |

## 7. 운영 행위
프락시움을 돌보는 일은 셸이나 DB에 직접 들어가서 하지 않아요. 정해진 목록의 행위이고, 모두 API이고, 명령줄과 MCP로도 나요. 행위는 프락시움 자신의 상태만 바꿔요. 소스와 현실 시스템에는 읽기만 해요(ADR-0001).

### 7.1 목록
축은 ADR-0007의 넷이에요: 바꾸나 · `reversibility` · `disruption` · 사람 손(모든 운영 행위가 「아니요」라 표에서 뺐어요). 「소스」 범위의 행위는 그 소스를 범위에 가진 `operator`가 해요.

| 행위 | 하는 일 | 축 | 선언한 효과 | 되돌리는 행위 | 필요한 역할 | 승인 기본 |
|---|---|---|---|---|---|---|
| `pause_source`, `pause_feed`, `pause_stream` | 소스, 공급, 흐름을 멈춰요 | 예 · reversible · none | 새 수집이 멈추고 `paused`로 보여요. 신선도는 나빠지고 묻기는 그대로 | `resume_source`, `resume_feed`, `resume_stream`(일정이 저절로 멈춘 소스도 이어요) | `operator`(소스) | 아니요 |
| `release_held_stream` | 붙잡힌 흐름의 쌓인 변경을 받은 순서대로 적용 | 예 · irreversible · none | `held`가 풀리고 새 KG 버전이 나와요 | 없음(새 변경으로 앞으로 고쳐요) | `operator`(소스) | 예 |
| `discard_held_stream` | 쌓인 변경을 버리고 전부 받아 맞춰요(맞출 수 있는 소스만) | 예 · irreversible · data_loss | 쌓인 것이 사라지고 맞추기가 돌아요 | 없음 | `operator`(소스) | 예 |
| `rerun_sync`, `cancel_sync` | 실행을 다시 하기, 도는 실행을 취소 | 예 · reversible · none | 새 실행이 서거나 멈춰요. 안전장치는 그대로 | 서로 | `operator`(소스) | 아니요 |
| `drain_worker` | 일꾼 한 벌이 새 일을 안 쥐고 하던 일만 끝내요 | 예 · reversible · none | 그 벌이 `drained`가 되고 줄은 다른 벌이 가져가요 | `undrain_worker` | `operator`(전체). 전부 비우기는 `admin` | 아니요(넓으면 예) |
| `rebuild_projection` | 투영을 일꾼이 다시 짓고 엔진이 갈아 끼워요 | 예 · reversible · none | 그동안 옛 투영으로 답해요. 메모리가 두 배 필요해요 | 필요 없음(파생물) | `operator`(전체) | 아니요 |
| `roll_engine` | 엔진을 한 벌씩 내리고 올려요(다 올라온 뒤 다음 벌) | 예 · reversible · none | 벌마다 `started`를 거쳐요 | `roll_engine` | `operator`(전체) | 아니요 |
| `pin_serving`, `unpin_serving` | 물음을 옛 KG 버전에 고정하고 풀어요(`pending` → `active`) | 예 · reversible · none | 답의 `as_of.pinned`가 보여요 | 서로 | `operator`(전체), `admin` | 아니요 |
| `run_archive_now`, `run_purge_now` | 기한이 지난 것을 오래된 것의 자리로 내려 두기 / 지우기 | 예 · reversible · none / irreversible · data_loss | 지금 자리의 크기가 줄어요(내린 것도 읽혀요) / 지운 만큼 줄어요 | 필요 없음 / 없음 | `operator`(전체) / `admin` | 아니요 / 예 |
| 저장소 행위(줄이기, 붙잡기, 뒤채움 멈추기, 긴 트랜잭션 끊기, 질의 꼴 막기, 표 청소) | 저장소의 신호가 가리킬 때 PostgreSQL을 돌봐요. 이름과 축은 구현하는 변경이 목록에 더해요 | 예 · 행위마다 | 신호(`storage.*`)에 나타나요 | 행위마다 | `operator`(전체), 넓으면 `admin` | 위험을 따라요 |
| `run_connection_check_now` | 소스에 닿아 보고 결과만 기록(소스 지키기를 거쳐요) | 아니요 · reversible · none | 소스의 연결 건강이 새로 나와요 | 필요 없음 | `operator`(소스) | 아니요 |
| `pause_sources_by_secret` | 비밀 값이 샌 것으로 의심될 때 그 값을 쓰는 소스를 한 번에 멈추고 값에 `suspected`를 표시해요 | 예 · reversible · none(받기만 멈추고 묻기는 돼요. 사람 손은 필요 없어요) | 그 값을 쓰는 소스가 모두 `paused`가 되고 새 연결이 없어요 | `resume_source`(값의 판을 물린 뒤 소스마다) | `operator`(그 소스들을 모두 범위에 가진), 넓으면 `admin` | 아니요(되돌릴 수 있고 지키는 행위예요. 설정으로 켤 수 있어요) |
| `rotate_system_token` | 시스템 계정의 토큰을 갈아요 | 예 · irreversible · service_affecting | 옛 토큰은 겹침 뒤에 막혀 그것을 쓰던 시스템이 끊겨요. 저장소가 살아 있으면 거둠은 30초 안에 퍼져요 | 없음 | `admin` | 예 |
| `rewrap_master_key` | 데이터 열쇠를 새 으뜸 열쇠로 다시 감싸요(ADR-0018) | 예 · reversible · none | 옛 으뜸 열쇠를 물려도 되는 때가 와요. 값은 다시 암호화하지 않아요 | 옛 으뜸 열쇠로 다시 감싸요 | `admin` | 예 |
| `redact_raw_record` | 원본 기록의 내용을 걷어 내요(해시와 자리와 까닭만 남겨요) | 예 · irreversible · data_loss | 그 기록을 다시 돌리면 값은 걷어 낸 것으로 다뤄요 | 없음 | `admin` | 예 |
| `enter_maintenance_mode`, `exit_maintenance_mode` | 고치는 일(정의의 고치기, 제안, 병합)을 받지 않기 / 다시 받기. 묻기와 수집·적용은 그대로 | 예 · reversible · service_affecting / none | 고치는 요청이 `blocked.maintenance_mode`로 막혀요 / 풀려요 | 서로 | `admin` | 예 / 아니요 |
| `update_setting`, `revert_setting` | 설정을 바꾸고 되돌려요(8절) | 예 · reversible · none | 새 판이 서고 역할이 다음에 읽을 때 써요 | 서로 | `admin` | 설정 항목의 위험을 따라요 |

토큰 값과 비밀 값은 어느 행위의 결과에도 실리지 않아요. 저장소 행위는 알파에 이름과 위험의 축만 계약으로 두고 구현은 클로즈 베타에서 오픈 베타예요(13절). `pause_sources_by_secret`은 판을 물리고 값을 바꾸는 일과 풀어 쓴 기록을 뽑는 일(ADR-0018)에 앞서 하는 첫 조치예요. 새 토큰은 계정을 다루는 `admin`의 길이 줘요(access-control 11절).

### 7.2 미리 해 보기
바꾸는 행위는 실제로 하지 않고 무엇이 일어날지만 답해요(`POST /v1/ops/actions/{action}:dry-run`, 안전함). 답은 계산한 영향을 선언한 효과와 나란히 적고, 선언한 축보다 무거운 영향이 계산되면 무거운 쪽이 이겨요(`raised_by`에 까닭). 마지막 한 벌을 비우거나 소스가 읽은 자리부터 다시 줄 수 없는 흐름을 멈추면 축이 올라 승인이 필요해져요. 칸: `dry_run_id`와 `expires_at`(`ops.dry_run_ttl_s`. 실행을 만들 때까지의 시간이에요. 그 사이 대상이 바뀌면 `stale.dry_run_outdated`로 거절하고, 승인을 기다리는 동안은 대상이 바뀌었는지만 다시 봐요), `would_do`(할 일의 단계), `effects`(`declared` 선언한 효과, `computed` 무엇이 멈추나와 무엇이 걸리나를 센 것), `risk`와 `raised_by`, `blocked_by`(겹침, 점검 모드, 권한, 너무 넓음), `requires`(필요한 역할, 승인 필요와 까닭), `reverse_action`, `verification_plan`(뒤의 확인에서 볼 신호와 기대).

### 7.3 너무 넓은 행위를 막는 규칙
- 대상이 「전부」이거나 대상의 비율이 `ops.max_targets_ratio`(기본 25%)를 넘으면 `admin`만 하고 승인이 필요해요(일꾼을 전부 비우기, 모든 소스를 멈추기).
- 한 대상에는 한 번에 한 행위만 돌아요. 같은 계열이 돌고 있으면 `conflict.action_in_progress`예요. 만들기는 `Idempotency-Key`를 받아요(같은 열쇠와 같은 본문이면 처음 실행을 줘요). 점검 모드 동안 고치는 행위(`update_setting` 등)는 `admin`만 해요.

### 7.4 실행의 흐름과 뒤의 확인
`POST /v1/ops/actions/{action}/runs`가 실행 하나(`action_run`)를 만들어요. 오래 걸리면 작업 자원(`/v1/jobs`)이 따라요.
```
 requested ─▶ awaiting_approval ─▶ approved ─▶ running ─▶ verifying ─▶ succeeded
                │      └▶ rejected / expired        └▶ failed / canceled   └▶ effect_not_confirmed
                └(승인이 필요 없으면 바로 approved)
```
`action_run`의 칸: `run_id`, `action`, `params`, `target`, `requested_by`(`actor_kind`, `actor_id`), `diagnosis_id`와 그 요약(`rule_id`와 근거의 참조. 진단이 사라져도 기록이 남게 복사해요), `dry_run_id`, `approval`(`required`, `reason`, `approved_by`, `at`), `idempotency_key`, `state`, `result`(`outcome`, 오류 `code`), `verification`, `reverse_of`, `trace_id`.

**뒤의 확인.** 선언한 효과를 신호의 기대로 바꾼 것이 `verification_plan`이에요. 실행이 끝나고 `ops.verification_settle_s`(14절) 뒤부터 신호를 보고 판정을 결과에 적어요: `confirmed`(기대한 모양이 나타났어요), `not_observed`(기한 안에 안 나타났어요), `contradicted`(반대로 나빠졌어요), `unknown`(신호가 없어 판정 못 했어요). `confirmed`가 아니면 상태는 `effect_not_confirmed`이고 알림이 가요. **저절로 되돌리지 않아요.** 되돌리는 행위를 제안으로 붙일 뿐이고 하는 것은 밖의 에이전트나 사람이에요.

## 8. 설정은 데이터
운영의 설정(한도, 문턱, 보관 기한, 동시 수)은 파일을 고쳐 다시 띄우는 값이 아니라 API로 바꾸는 버전이 있는 값이에요. 저장소에 닿는 법처럼 설정을 읽기 전에 필요한 값은 이 목록에 없고 배포가 줘요. 칸: `key`(점으로 이은 snake_case. `worker.candidate_run_concurrency`), `type`(`int`, `duration`, `ratio`, `bool`, `enum`), `unit`, `min`, `max`, `default`(범위 밖은 `invalid_input.field_range`로 거절해요), `applies`(`live` 역할이 다음에 읽을 때 써요 / `restart_required` 다시 띄워야 써요), `risk`(바꿀 때의 위험의 축. 기본 reversible · none), `rev`, `value`, `previous_value`, `changed_by`, `changed_at`, `reason`, `run_id`, `diagnosis_id`.
- **바꾸는 흐름.** `update_setting` 하나가 길이에요. `PATCH /v1/settings`도 그 짧은 모양이라 같은 미리 해 보기, 승인, 감사를 거쳐요. 미리 해 보기는 범위 검사, 쓰는 역할과 벌, 다시 띄울 벌의 목록을 보여요. `If-Match`가 앞 판과 맞을 때만 새 판이 서요.
- **되돌리기.** `revert_setting`은 앞 판의 값으로 새 판을 만들어요(지우거나 되감지 않아요).
- **다시 띄워야 먹는 설정.** 새 판이 서도 벌마다 `setting_rev_applied`가 올라야 써요. 그때까지 `pending_restart`로 올리는 중의 상태(12.2)에 보여요.
- **정의와의 경계.** 소스를 지키는 값, 안전장치의 값, 알림의 규칙, 목표는 정의의 항목이라 브랜치와 제안으로 고쳐요(ADR-0014). 이 절의 설정은 제품의 운영에 걸리고 정의가 아니에요. 보관 기한(지표, 감사 기록 등)도 여기에 들어요. 기본값은 [기본값 표](defaults.md)에 있어요.
- 예(기본값(설정)): `worker.candidate_run_concurrency`(2, `live`), `worker.db_pool_size`(16, `restart_required`), `ops.dry_run_ttl_s`(600), `ops.max_targets_ratio`(0.25), 행위마다 `ops.action.<행위>.require_approval`과 `.delegation`(9.2).

## 9. 권한과 기록
### 9.1 역할과 범위
ADR-0012의 역할과 범위를 그대로 써요. 읽기에도 새 역할이 없어요.

| 하는 일 | 역할과 범위 |
|---|---|
| 부품의 건강, 신호, `diagnose`, 사건 코드의 목록 읽기 | 전체 범위의 `operator`, `admin`, `auditor`(부품은 데이터의 타입 범위로 가를 수 없어요). `live`, `ready`, `started`의 거친 답은 로그인 없이 |
| 소스에 딸린 행위(멈추기, 잇기, 다시 하기, 연결 점검, 붙잡힌 흐름) | 그 소스를 범위에 가진 `operator` |
| 제품 전체에 걸린 행위(투영, 엔진, 고정, 내려 두기, 일꾼 한 벌) | 전체 범위의 `operator`, `admin` |
| 넓은 행위, 설정, 점검 모드, 지우기, 토큰 | `admin` |
| 승인 | 요청한 쪽이 아닌 `admin`이나 범위를 가진 `operator`인 **사람**. 시스템 계정은 승인하지 못해요 |

### 9.2 시스템 계정(에이전트)과 승인의 설정
에이전트는 시스템 계정이에요(예: `acct_ops_agent`). 사람과 같은 역할과 범위를 받고, 토큰은 해시로만 저장되고, 만료, 교체, 호출 한도가 있어요(ADR-0012). 계정의 `contacts`가 한도와 이상한 쓰임을 받아요. 에이전트는 승인할 수 없어요. 계정을 끄거나 토큰을 거두는 것은 저장소가 살아 있으면 30초 안에 퍼져요(권한 사본의 나이 상한 15분이 그 늦은 쪽의 한계예요). 시스템 계정의 `on_behalf_of`는 관리자가 그 계정에 묶어 둔 사람 가운데서만 골라요. 행위마다 설정 둘이 있어요.
- `require_approval`: 승인이 있어야 실행해요. 기본값은 위험에서 와요. 되돌릴 수 없거나(`irreversible`) 서비스가 끊기는(`service_affecting`, `data_loss`) 행위는 참이에요. 요청한 주체가 사람이든 시스템 계정이든 같아요.
- `delegation`: 시스템 계정이 그 행위를 어디까지 하나. `none`(못 해요, 기본) / `with_approval`(요청하고 사람이 승인해요) / `autonomous`(승인 없이 해요). `autonomous`는 되돌릴 수 있고 서비스가 끊기지 않는 행위에만 설정돼요. 아니면 `invalid_input.autonomy_exceeds_risk`로 거절해요.

### 9.3 감사 기록
운영 행위는 모두 감사 기록(ADR-0012)에 남아요. 읽기(신호, `diagnose`)는 보통의 읽기라 남기지 않아요. `action`은 `ops_action_requested`, `ops_action_approved`, `ops_action_rejected`, `ops_action_finished`, `setting_changed`이고, 기존 칸(`actor_kind`, `actor_id`, `target_*`, `outcome`, `request_id`)에 이 칸을 더해요: `ops_run_id`, `diagnosis_id`와 `diagnosis_rule_ids`(어떤 진단을, 어느 규칙이 맞아서 근거로 했나. 사람이 근거 없이 한 것은 비어 있고, 시스템 계정의 요청이 비면 `invalid_input.diagnosis_required`), `dry_run_id`, `verification_verdict`. 비밀 값을 언제 어느 역할의 어느 실행이 풀어 썼는지(풀어 쓴 기록)는 ADR-0018의 감사 칸이고, `secret_unwrap_count`가 그 수를 세요.
```json
{"audit_id":"au_9d1","occurred_at":"2031-04-02T07:59:30Z","actor_kind":"system_account","actor_id":"acct_ops_agent","action":"ops_action_finished","target_kind":"setting","target_id":"worker.candidate_run_concurrency","outcome":"allowed","ops_run_id":"ar_204","diagnosis_id":"dg_5521","diagnosis_rule_ids":["r_storage_conn_01"],"verification_verdict":"confirmed"}
```

## 10. 에이전트가 쓰는 면
### 10.1 운영의 MCP 면
KG를 읽는 MCP(닫힌 질의 열셋 등)와 **따로** 낸 면이에요. 둘은 같은 쓰임 위의 어댑터라는 규칙(ADR-0016)을 따르고 권한의 판단과 오류 코드가 같아요. KG의 MCP는 읽기만 하고 이쪽에는 행위 툴이 있어요. 모두 MCP 게이트웨이에 등록되는 툴이에요. **프락시움 안에는 진단을 보고 행위를 골라 하는 루프를 두지 않아요.** 돌보는 것은 밖의 에이전트예요. 이미 든 정해진 안전장치(점유를 이어받기, 잇달아 실패하면 일정 멈추기)는 남아요.

**운영 행위 하나가 MCP 툴 하나예요.** 행위의 이름을 인자로 받는 툴 하나로 뭉치지 않아요. 툴의 허용 목록만 할 수 있는 게이트웨이가 행위마다 막고 열 수 있게 하려는 거예요. 툴 이름은 `ops_` 뒤에 7.1절의 행위 이름을 붙여요(예: `ops_drain_worker`, `ops_update_setting`, `ops_pause_sources_by_secret`). 인자는 `params`, `dry_run_id`, `diagnosis_id`이고 승인이 필요하면 `awaiting_approval`로 돌아와요.

| 툴 | 종류 | 하는 일 |
|---|---|---|
| `ops_get_signals`, `ops_list_components`, `ops_get_component_health` | 읽기 | 부품의 신호(`/v1/metrics/query`의 `component`)와 건강(`/v1/health`) |
| `ops_diagnose`, `ops_list_event_codes`, `ops_get_event_code` | 읽기 | 5.3의 진단, 6절의 목록 |
| `ops_list_actions`, `ops_get_settings`, `ops_get_rollout`, `ops_get_synthetic_checks`, `ops_list_action_runs`, `ops_get_action_run` | 읽기 | 행위의 목록, 설정, 올리는 중의 상태, 스스로 재기의 결과, 실행과 뒤의 확인 |
| `ops_dry_run_action` | 읽기(안전함) | 7.2. 행위의 이름을 받아 무엇이 일어날지만 답해요 |
| `ops_<행위>` | **행위** | 7.1의 행위 하나마다 툴 하나예요. 실행을 만들어요 |

승인과 거절의 툴은 없어요. 사람이 API(`:approve`)와 명령줄로 해요. 답의 크기 상한과 다음에 할 일을 말하는 오류는 KG의 MCP와 같아요(ADR-0010).

### 10.2 끝까지 따라가는 지어낸 예시
설정은 단계 둘째(제안과 승인, 13절)이고 `update_setting`에 `with_approval`이 걸려 있어요.

**1. 알림.** 에이전트가 알림 `a_311`을 받아요: 엔진의 물음 응답 시간 목표가 빠르게 나빠짐(p95 2.4초, 목표 1초).

**2. 진단.** `ops_diagnose(symptom_ref={kind: alert, id: a_311})`
```yaml
diagnosis_id: dg_5521   catalog_version: "1.4.0"   persisted: true
coverage: {reported: [api-1, api-2, wk-1, wk-2, wk-3, en-1, en-2], missing: []}
findings:
  - component: {kind: component, id: engine}   state: degraded   symptom: objective.latency_burning
    suspected_causes:
      - {code: engine.projection_lagging, component: en-1, rule_matched: {rule_id: r_engine_lag_01}}
      - {code: apply.lag_high, component: worker, rule_matched: {rule_id: r_apply_lag_02}}
      - {code: storage.connections_exhausted, component: storage, root: true, rule_matched: {rule_id: r_storage_conn_01}}
    evidence: [{signal: projection_lag_s, resource: en-1, observed: 720, threshold: 30, window: 10m}, {signal: apply_lag_seq, resource: worker, observed: 41200}, {signal: db_pool_in_use, resource: wk-2, observed: 16, threshold: 16}, {signal: db_pool_waiting, resource: wk-2, observed: 9}]
    actions: [{action: update_setting, params: {key: worker.candidate_run_concurrency, value: 1}, reason_rule: r_storage_conn_01, requires_approval: true}, {action: drain_worker, params: {instance_id: wk-2}, reason_rule: r_storage_conn_01, requires_approval: false}]
```
에이전트가 읽어요: 물음이 느린 것은 엔진의 투영이 12분 뒤처졌기 때문이고, 일꾼이 저장소의 연결이 차서(풀 16 중 16, 후보 실행 둘이 쥐고 있어요) 적용을 못 해요.

**3. 미리 해 보기.** `ops_dry_run_action(update_setting, {key: worker.candidate_run_concurrency, value: 1}, diagnosis_id: dg_5521)`
```yaml
dry_run_id: dr_88   expires_at: "2031-04-02T07:56:00Z"
would_do: ["설정 판 14 → 15", "wk-1, wk-2, wk-3이 다음 읽기(30초 이내)에 새 값을 써요"]
effects: {declared: "새 후보 실행의 동시 수가 줄어 일꾼의 연결 점유가 내려가요", computed: {running_candidate_runs: 6, stopped_now: 0, queued_after: 4}}
risk: {mutates: true, reversibility: reversible, disruption: none, physical: false}   raised_by: []
requires: {role: admin, approval: true, reason: "ops.action.update_setting.require_approval"}
reverse_action: {action: revert_setting, params: {key: worker.candidate_run_concurrency, to_rev: 14}}
verification_plan: [{signal: db_pool_in_use, resource: wk-2, want: "< 13", within_s: 600}, {signal: apply_lag_seq, resource: worker, want: "줄어든다", within_s: 900}]
```

**4. 요청과 승인.** `ops_update_setting(..., dry_run_id: dr_88, diagnosis_id: dg_5521)`가 `{run_id: ar_204, state: awaiting_approval}`를 돌려줘요. 승인 요청이 `admin`에게 가고 미리 해 보기와 진단의 요약이 붙어요. 사람이 읽고 `POST /v1/ops/action-runs/ar_204:approve`로 승인해요. 에이전트는 기다리고 상태의 바뀜은 사건의 길로 받아요.

**5. 실행.** 설정 판 15가 서고 일꾼 셋이 새 값을 읽어요(`running`). 하던 후보 실행 여섯은 끝나기를 기다려요.

**6. 뒤의 확인.** 9분 뒤에 신호를 봐요: `verification: {verdict: confirmed, observed: [{signal: db_pool_in_use, resource: wk-2, value: 9}, {signal: apply_lag_seq, value: 3100, trend: "줄어든다"}]}`. `ar_204`는 `succeeded`가 되고 알림 `a_311`은 조건이 풀려(연속 3번) `resolved`가 돼요.

**7. 기록.** 감사 기록 `au_9d1`(9.3)에 누가(`acct_ops_agent`가 요청, 사람이 승인), 무엇을, 어떤 진단과 규칙(`dg_5521`, `r_storage_conn_01`)을 근거로, 결과(`confirmed`)가 남아요. `confirmed`가 아니었다면 에이전트는 `reverse_action`을 같은 길로 요청했을 거예요.

## 11. 프락시움도 KG의 객체
제품에 든 소스 `praxium_self`가 프락시움의 구성을 **시스템 KG**에 적어요. 타입은 예약된 넷째 이름 공간 `sys`의 것이고 층은 닫힌 목록의 열째 값 `system`이에요. `sys` 타입은 표준 모델의 데이터 파일과 따로 제품이 싣고, 시스템 KG는 `sys` 항목만 쓰는 정의 묶음을 가리켜요(정의의 본줄기는 배포에 하나예요). 스스로 재기(4절)의 지어낸 객체도 이 KG에 담겨요. 담기는 것은 프락시움 자신(역할의 벌, 저장소, 받은 변경의 기록, 파일 보관, SSO)과 기댐(`hard` / `soft`)이에요. 벌의 존재는 역할의 등록에서(`observed`), 기댐은 제품에 실린 표에서(`declared`) 와요. 상태가 아니라 구성이에요.

`admin`이 원하면 이 구성을 운영의 KG에도 내보내 인프라와 이을 수 있어요(설정 `ops.self_kg.export_to_main`, 기본 꺼짐. 구현은 클로즈 베타예요). 이으면 「이 저장소가 죽으면 무엇이 멈추나」를 `impact`로 물을 수 있어요. 계획과 설계 검토에 쓰는 거예요.

**이것에 기대어 프락시움을 살피지 않아요.** 프락시움이 죽으면 그 KG에도 물을 수 없어요. 구성은 받아서 짓는 데 시간이 걸려 지금의 고장을 따라가지 못해요. 같은 파이프라인을 쓰므로 파이프라인이 아프면 이 KG도 같이 묵어요. 살피는 길은 5.4절과 밖으로 내는 신호예요.

## 12. 지켜보는 것을 지켜보기, 올리는 중, 늘어남
### 12.1 지켜보는 것을 지켜보기
신호를 밖으로 내는 길과 알림을 보내는 길이 죽으면 아무도 모르고 평안한 것처럼 보여요. 그래서 정해진 간격(`ops.heartbeat_interval_s`, 기본 60초)으로 늘 있는 생존 신호를 두 길 끝까지 보내요. 지표는 `export_last_success_age_s`, `export_queue_depth`, `export_dropped`(보내지 못해 버린 수), `alert_heartbeat_age_s`와 `health_evaluator_age_s`(13절), `e2e_probe_age_s`예요. 내보내기는 서비스를 막지 않아요. 크기가 정해진 버퍼가 차면 오래된 것부터 버리고 `export_dropped`로 세요. **밖에서 할 일:** 오지 않으면 밖에서 안다는 것이 요점이라, 조직의 관측 도구가 위 나이가 간격의 3배(기본값)를 넘거나 생존 신호가 끊긴 것을 보고 제품과 따로 알려야 해요.

### 12.2 올리는 중의 상태
`GET /v1/ops/rollout`이 줘요. 이웃한 두 버전의 역할은 함께 돌 수 있어야 해요. 스키마의 옮김은 넓히고(새 것을 더함) 옮기고 좁히는(옛 것을 지움) 차례로 하고 좁히기는 다음 버전에서 해요. 표를 다시 쓰는 변경은 하지 않고 잠금 대기 한도를 첫 옮김부터 걸어요. 표준 모델과 `sys` 타입도 옮김의 한 걸음으로 저장소에 실려서 벌마다 본줄기의 해시가 달라지지 않아요. 적용의 점유에는 실행기의 의미 버전이 들어서 낮은 버전의 일꾼이 적용을 쥐지 못해요. 시점 복구를 하면 KG의 세대 번호가 올라가고 `as_of`, 커서, 사건 ID, `min_seq`에 실려요(다르면 `conflict.generation_changed`). 역할 사이의 버전이 `ops.version_skew_alert_s`(기본 3600초) 넘게 어긋나면 `rollout.version_skew_long`으로 알려요. 켜진 기능은 `/v1/capabilities`의 `features`와 같은 것이에요.
```yaml
schema: {current: 41, target: 42, state: migrating, step: "3/5"}
roles: [{role: api, instance_id: api-1, version: "0.9.2", catalog_version: "1.4.0", api_version: v1, state: ready}, {role: worker, instance_id: wk-1, version: "0.9.1", state: draining}]
skew: {versions: ["0.9.1", "0.9.2"], skewed: true, since: "2031-04-02T06:10:00Z"}
features: [{feature: self_kg, enabled: false}]   settings_pending_restart: [{key: worker.db_pool_size, rev: 9, instances: [wk-2]}]
```

### 12.3 늘어남을 미리 보기
저장소의 크기, 줄의 길이, 메모리는 정해진 방법으로 내다봐요: 지난 창(기본 14일)의 일별 최댓값에 최소제곱 직선을 맞추고, 기울기가 양수이면 `time_to_limit_s` = (한도 − 지금) ÷ 기울기예요. 같은 입력이면 같은 값이에요. 한도는 배포가 알려 주는 설정이고 모르면 내다보지 않아요(`unknown`). 예: 저장소가 하루 12GB씩 늘고 한도 600GB에 480GB이면 10일이고 `storage.growth_limit_near`를 알려요(문턱은 14절). 한계: 계단식으로 느는 것과 요일 무늬는 따라가지 못해요.

## 13. 단계
맡김의 단계는 셋이고, 제품의 단계는 넷(알파, 클로즈 베타, 오픈 베타, 정식)이에요. 알파는 둘로 나뉘어요. 알파 1(뼈대와 한 줄)에는 이 문서의 것이 없고 임시 콘솔만 있어요. 알파 2에서 신호, 핵심 진단, 행위, 설정, 감사가 API와 명령줄로 돼요. 임시 콘솔도 프락시움 자신의 진단을 보여요(공개된 API만 써서요).

**계약의 자리만 알파에 두고 구현은 클로즈 베타로 내리는 것**: 목표의 소진율과 「빠르게 나빠짐」, 운영의 MCP 면과 `diagnose`의 규칙 묶음 전부(알파는 핵심 규칙만), 시스템 KG를 운영 KG에 내보내기. 칸, 길, 코드의 목록은 알파부터 있어요. 저장소의 신호는 이름과 단위가 알파부터 계약이고 구현은 단계별이에요. 저장소 행위는 클로즈 베타에서 오픈 베타예요. 한도까지 남은 시간의 내다봄(12.3)은 오픈 베타예요. 비밀 값 쪽은 풀어 쓴 기록과 만료 알림이 클로즈 베타, 으뜸 열쇠의 건강, 칸 암호화의 점검, 급할 때의 행위가 오픈 베타예요(ADR-0018).
| 단계 | 에이전트가 하는 일 | 설정 | 사람이 하는 일 |
|---|---|---|---|
| 1. 보기와 진단 | 신호를 읽고 `diagnose`를 해요. 행위는 못 해요 | 모든 행위가 `delegation: none` | 진단을 보고 직접 해요 |
| 2. 제안과 승인 | 미리 해 보기를 하고 행위를 요청해요 | 고른 행위를 `with_approval` | 승인하고 뒤의 확인을 봐요 |
| 3. 낮은 위험은 스스로 | 되돌릴 수 있고 서비스가 안 끊기는 행위를 스스로 해요 | 그 행위만 `autonomous` | 뒤의 확인과 감사를 보고, 안 맞으면 `none`으로 내려요 |

옮기는 것은 설정이고 자동으로 오르지 않아요. **지금 만드는 자리**는 행위의 목록, 위험의 축, 승인 필요, 맡김의 설정, 감사 기록의 칸이에요. **E2E 프로세스 때 얹힐 것**은 맡김의 틀 자체(실적이 쌓이면 올리고 무효가 되면 내리는 규칙, 업무 단위의 적용 단계)예요. 방향은 [방향 메모](process-direction.md)에 있고 아직 결정이 아니에요.

## 14. 기본값(설정)과 재는 일
이 문서의 숫자는 제품이 싣는 기본값(설정)이에요. [기본값 표](defaults.md)에 있는 것(보관, 권한 사본의 나이 등)은 그 표를 따르고, 아래는 그 표에 없는 이 문서의 값이에요. 알파는 객체와 링크 100만 개 수준의 지어낸 세계로 기능을 시험하고, 재서 고치는 일은 클로즈 베타에서 해요.
| 숫자 | 기본값 | 클로즈 베타에서 무엇으로 재나 |
|---|---|---|
| 스스로 재기의 간격과 제한 시간, 자신의 목표(3.5) | 흐름 60초, 배치 15분, 60초, 표의 값 | 지어낸 세계에서 응답 시간과 밀림의 분포, 거짓 알림 |
| 역할이 메모리에 쥐는 최근 신호의 길이, 추적의 표본 비율 | 정하지 않음 | 메모리와 추적의 비용, 진단에 필요한 창 |
| 생존 신호의 간격과 밖의 기준, 설정 사본을 새로 읽는 간격 | 60초, 3배, 30초 | 알림 길의 지연, 설정이 퍼지는 시간 |
| 미리 해 보기의 유효 시간, 뒤의 확인을 기다리는 시간 | 600초, 정하지 않음 | 행위마다 효과가 나타나는 시간 |
| 넓은 행위의 비율, 버전 어긋남의 기한, 내다보는 창 | 25%, 3600초, 14일 | 알파의 운영, 저장소의 실제 늘어남 |

## 15. 알고 가는 한계와 아직 정하지 않은 것
**알고 가는 한계**
- 진단은 닫힌 규칙이라 규칙에 없는 새 고장의 까닭은 `unknown_cause`예요. 그때 에이전트는 신호를 직접 읽어요. 규칙은 사고마다 더해져요(목록의 버전이 올라요).
- 스스로 재기는 작은 그래프 하나의 길만 재요. 큰 KG의 느림은 부품의 신호로 봐요. 5.2의 인과처럼 몇 가지는 가정이라, 지어낸 세계에서 고장을 일으켜(연결 풀 고갈, 일꾼 정지, 투영 지연) 규칙이 맞는지 재요.
- 일꾼과 엔진이 모두 멈추면 신호의 일부가 비어요. 비어 있음은 모름이고 정상이 아니에요.
- 저장소 없이 답하는 동안 권한 사본이 15분을 넘으면 기본 부여의 읽기만 돼요(5.4).

**아직 정하지 않은 것**
- `sys` 타입의 구체적인 목록과 링크 타입의 이름, 추적의 표본 규칙과 보관 기한, 저장소 행위의 이름과 축, 메모리에 쥐는 신호의 길이.

## 16. 관련 결정과 요구사항
- 결정: [ADR-0017](adr/0017-self-observability-and-operability.md). 이어진 것은 ADR-0006(역할 넷), ADR-0007(위험의 축), ADR-0010(MCP, 닫힌 질의), ADR-0011(가용성), ADR-0012(역할, 시스템 계정, 감사), ADR-0013(건강, 알림, 지표 표), ADR-0016(REST 규칙).
- 요구사항: REQ-200(역할 넷의 신호), REQ-201(스스로 재기), REQ-202(건강과 `diagnose`), REQ-203(사건 코드 목록), REQ-204(운영 행위), REQ-205(설정), REQ-206(권한과 기록), REQ-207(운영의 MCP 면), REQ-208(생존 신호, 올리는 중, 내다봄).
- 같이 읽을 문서: [데이터 관측성](data-observability.md), [KG 운영](kg-operations.md), [접근 제어](access-control.md), [REST API 자원과 길 (2)](rest-api-resources-graph.md), [KG에 묻기](kg-queries.md).
