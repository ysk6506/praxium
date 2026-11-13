---
title: 프락시움 자신의 관측성과 운영 가능성 — 신호, 진단, 운영 액션, 권한과 기록
kind: guide
status: active
follows:
  - docs/design-docs/adr/0017-self-observability-and-operability.md
---
# 프락시움 자신의 관측성과 운영 가능성

제품 책임자의 요구는 다음과 같아요. 프락시움 백엔드 자체에 대한 관측성도 확보돼야 하고, 나중에 AI 에이전트가 프락시움도 자율 운영할 수 있어야 해요. 이 문서는 프락시움이 정상 동작하는지 확인하고, 이상 상태이면 원인을 가려내고, 정해진 운영 액션으로 조치하고, 그 기록을 남기는 설계예요. 소스에서 들어온 데이터가 믿을 만한지는 [데이터 관측성](data-observability.md)(ADR-0013)이 맡아요. 숫자는 기본값(설정)이고 [기본값 표](defaults.md)와 14절에 있어요.

## 1. 목적과 다루는 질문
| 쟁점 | 답 |
|---|---|
| 프락시움이 정상 동작하는가, 무엇이 왜 이상 상태인가 | 역할 4종의 신호, 자체 측정, 헬스 상태와 `diagnose`, 이벤트 코드 목록(3~6절) |
| 에이전트가 무엇을 해도 되는가, 한 일이 기록으로 남는가 | 운영 액션 목록, 위험 축, 승인 필요 여부, 설정, 감사 로그(7~9절) |
| 프락시움이 비정상일 때도 프락시움에 질의할 수 있는가 | 스토리지 없이 답하는 범위, 감시 대상 감시(5, 12절) |
| 에이전트는 어떤 인터페이스로 접근하는가 | 운영 MCP 인터페이스(10절) |

## 2. 개요
에이전트가 운영하려면 네 가지가 필요해요. 지표를 내보내는 것만으로는 부족해요.
```
 ① 신호                  ② 진단                  ③ 운영 액션                   ④ 권한과 기록
 역할 4종의 지표·로그·트레이스 ─▶ `diagnose`           ─▶ 목록의 액션 ─▶ 미리 실행 ─▶ 역할·범위·승인 필요 ─▶ 실행
 자체 측정(합성 점검)          일치한 규칙과 근거        위험 축 4개, 선언한 효과       감사 로그(근거로 삼은 진단)
       ▲                    이벤트 코드 목록(계약)                                        │
       └─────────── 사후 확인: 선언한 효과가 신호에 나타났는가 ◀────────────────────────────┘
 (외부 에이전트가 MCP 게이트웨이를 거쳐 ②③을 호출해요. 프락시움 내부에는 스스로 수정하는 루프가 없어요)
```
**ADR-0013과의 역할 분담.** ADR-0013은 데이터(소스, 데이터셋, 피드, 타입, 속성)의 헬스 상태와 정의 계보를 따라간 원인을, 이 문서는 시스템(`component`: 역할 인스턴스, 의존 대상, 큐, 프로젝션)의 헬스 상태와 부품 의존을 따라간 원인을 다뤄요. 지표 테이블 `metric_point`, 헬스 상태 6종, 알림, 문제 목록은 같은 틀을 써요. 둘은 데이터 측의 증상(지연, 무수신)과 시스템 측의 원인(워커 장애, 연결 고갈)에서 만나요.

## 3. 신호
### 3.1 틀
- 요청은 수·오류·소요 시간(RED)으로, 자원은 사용률·포화·오류(USE)로 측정해요. 표의 「방법」 컬럼이 R(요청 수), E(오류), D(소요 시간), U(사용률), S(포화)예요. 지표는 `metric_point`에 `resource_kind: component`로 쌓이고 `resource_id`는 인스턴스의 `instance_id`나 의존 대상의 이름, `detail_key`는 고정 목록이에요.
- 이름은 영어 snake_case예요. ADR-0013 13절에 있는 이름(「13절」)은 그대로 써요. 열린 형식으로 내보낼 때 의미 관례가 있는 것(요청 수·소요 시간)은 그 이름으로 바꿔 내보내요. 어느 영역의 관례가 안정 단계인지는 구현할 때 확인해요.

### 3.2 역할마다 내는 지표
| 역할 | 지표 | 설명 | 단위 | 방법 |
|---|---|---|---|---|
| 모든 역할 | `instance_up`, `process_restarts`, `process_cpu_ratio`, `process_memory_bytes` | 인스턴스 기동 여부, 재시작 횟수, CPU와 메모리 사용량 | 0/1, 회, 비율, 바이트 | E, U |
| | `db_pool_in_use`, `db_pool_waiting`, `db_pool_max` | 스토리지 연결 풀의 사용 수, 대기 수, 크기 | 개 | U, S |
| | `dependency_up`, `dependency_call_errors`, `dependency_call_duration_ms` | 의존 대상(`detail_key`: `storage`, `change_log`, `file_store`, `sso`, `notifier`, `secret_provider`, `master_key`, `engine`, `api`)에 접속 가능한지(0/1/알 수 없음), 호출 오류, 호출 소요 시간 | 0/1, 건, ms | E, D |
| | `role_version_info` | 역할, 코드 버전, 이벤트 코드 목록 버전, 계약 버전(값은 항상 1이고 정보는 레이블에 담아요) | 1 | |
| API | `api_requests`, `api_request_errors`, `api_request_duration_ms`, `api_inflight_requests` | 경로 그룹(`detail_key`)별 요청 수, 오류(오류 `code`의 그룹), 응답 시간, 처리 중인 요청 | 건, ms | R, E, D, U |
| | `api_error_ratio`(13절), `api_rate_limited`, `grant_cache_age_s` | 오류 비율, 한도 초과로 제한된 호출(`detail_key`: `requests`, `concurrency`, `compute_budget`), 권한 캐시의 경과 시간(상한이 있어요) | 비율, 건, 초 | E, S |
| 워커 | `worker_alive`, `lease_takeovers`(13절), `stale_attempt_rejections` | 정상 동작 여부, 장애 난 작업을 이어받은 수, 시도 번호(`attempt`)로 차단된 지연 쓰기 | 0/1, 건 | E |
| | `queue_depth`, `oldest_job_age_s`(13절), `tick_lag_s` | 큐에 대기 중인 작업 수와 가장 오래 대기한 작업(`detail_key`는 작업 종류. 예: `sync`, `stream_apply`, `candidate_run`, `merge`, `retention`, `synthetic_probe`), 스케줄러 틱의 지연 | 건, 초 | S |
| | `jobs_started`, `job_failures`, `job_duration_s`, `apply_duration_s`, `candidate_run_duration_s` | 작업 수, 실패(`detail_key`가 오류 코드), 소요 시간, 적용 1회와 드라이 런의 소요 시간 | 건, 초 | R, E, D |
| 질의 엔진 | `engine_queries`, `engine_query_errors`, `engine_query_duration_ms` | 질의 이름(`detail_key`)별 수, 오류, 응답 시간 | 건, ms | R, E, D |
| | `engine_memory_bytes`, `engine_loaded_version_age_s`, `projection_lag_seq`(13절), `engine_overlay_size`, `engine_compaction_duration_s`, `engine_swap_duration_s` | 메모리, 로드된 버전의 경과 시간, 반영하지 못한 `change_seq` 수, 오버레이 크기, 컴팩션과 교체의 소요 시간 | 바이트, 초, 건 | U, S, D |
| 수집기 | `collector_alive`, `collector_last_seen_age_s`(3절 데이터 쪽), `collector_poll_errors`, `collector_poll_duration_s`, `collector_envelopes_sent` | 정상 동작 여부(본체에 작업을 요청하러 온 시점이 곧 하트비트), 소스 읽기의 오류와 소요 시간, 전송한 엔벨로프 | 0/1, 초, 건 | R, E, D |
| | `collector_outbox_depth`, `collector_outbox_oldest_age_s` | 전송하지 못해 쌓인 엔벨로프와 그중 가장 오래된 것 | 건, 초 | S |

수집기와 엔진은 자기 지표를 열린 형식으로 직접 내보내요(ADR-0011). 워커가 수집하지 못한 구간은 0이 아니라 빈 값(알 수 없음)으로 남아요.

### 3.3 프락시움에 고유한 지표
소스에서 엔진까지는 구간 3개가 이어져 있어요. 지연이 어느 구간에 있는지가 원인을 가르는 첫 단서예요.
```
 소스 ─[수집 지연]─▶ 수집 변경 로그 ─[적용 지연]─▶ KG(`applied_seq`) ─[프로젝션 지연]─▶ 질의 엔진
```
| 지표 | 설명 | 단위 |
|---|---|---|
| `ingest_lag_s` | 수집 지연. 소스가 끝 위치를 알려 주면 그 위치와 로그에 기록한 소스 커서의 차, 아니면 마지막으로 기록한 시점부터의 경과 시간(`detail_key`: `cursor_gap`, `last_write_age`) | 초 |
| `apply_lag_seq`, `apply_lag_s` | 적용 지연. 수집 변경 로그의 마지막 위치와 `applied_through`의 차(`ingest_seq` 개수), 아직 적용하지 못한 가장 오래된 변경의 경과 시간 | 건, 초 |
| `projection_lag_seq`, `projection_lag_s` | 프로젝션 지연. KG의 `applied_seq`(대량 적용은 읽기 워터마크 `readable_through`)와 엔진이 반영한 `change_seq`의 차와 그 경과 시간. 엔진은 `volatile` 값의 경량 변경 레코드도 함께 따라가고, 그 스트림의 지연도 같은 지표로 내보내요 | 건, 초 |
| `held_streams`, `held_oldest_age_s`, `held_backlog_ratio` | 보류된 스트림 수, 가장 오래된 것의 경과 시간, 쌓인 양 ÷ 보관 한도(ADR-0011) | 개, 초, 비율 |
| `merge_queue_depth`, `merge_oldest_wait_s` | 병합 큐의 길이와 가장 오래 대기한 제안 | 건, 초 |
| `storage_bytes`, `storage_growth_bytes_per_day`, `storage_connections_in_use`, `storage_connections_max` | 스토리지 크기(`detail_key`: `live`, `hist`, `raw_record`, `change_log`, `metric`, `audit`, `other`), 증가 속도, 스토리지 측에서 본 연결 수와 한도 | 바이트, 개 |
| `version_skew_age_s`, `time_to_limit_s` | 역할 간 버전 불일치가 지속된 시간(버전이 하나면 0), 현재 추세로 한도에 도달하기까지의 시간(12.3) | 초 |
| `secret_master_key_reachable`, `secret_expires_in_s` | 시크릿(ADR-0018): 마스터 키에 접근 가능한지(시크릿을 복호화하는 워커의 수집 부분과 수집기만 내보내요. API와 엔진은 복호화할 수 없어 값이 없는 것이 정상이에요), 시크릿 만료까지 남은 시간(`detail_key`는 시크릿의 `id`이고 값이 아니에요) | 0/1/알 수 없음, 초 |
| `secret_unwrap_count`, `secret_scrubbed_fields`, `tls_verify_disabled_sources`, `at_rest_encryption_enabled` | 복호화해 사용한 횟수(`detail_key`: 역할), 수집 시 제거한 시크릿 수, 인증서 검증을 끈 소스 수, 스토리지의 저장 데이터 암호화 활성화 여부(점검) | 건, 개, 0/1/알 수 없음 |
| `storage_dead_tuple_ratio`, `storage_xid_age`, `storage_wal_bytes_per_s`, `storage_replication_lag_s`, `storage_long_txn_age_s`, `storage_slot_retained_bytes`, `storage_retention_lag_s`, `storage_migration_lock_wait_s` | 스토리지 신호(8-26): dead tuple과 bloat, 트랜잭션 ID의 경과량, WAL 양, 복제 지연, 장기 트랜잭션, 복제 슬롯이 붙잡고 있는 양, 보존 기한 처리 지연, 마이그레이션의 잠금 대기. 이벤트 코드는 `storage.*`예요. 구현은 단계별(13절)이고 이름과 단위는 알파부터 계약이에요 | 비율, 번호, 바이트, 초 |
| `message_log_reachable`, `message_log_lag`, `message_log_write_errors`, `message_log_retention_margin_s` | 연결된 메시지 로그 스토리지(클로즈드 베타. 제품이 기동하지 않아요): 접속 가능 여부, 지연, 쓰기 실패, 보존 기한까지 남은 여유 | 0/1/알 수 없음, 건, 초 |
| `ops_action_runs`, `ops_action_verdicts` | 운영 액션의 실행 횟수(`detail_key`: 액션 이름과 결과)와 사후 확인의 판정 | 건 |

### 3.4 로그 레코드와 트레이스
로그 레코드 하나는 JSON 객체 하나예요. 필드: `ts`(UTC), `severity`(`debug` / `info` / `warning` / `error` / `critical`), `event_code`(안정된 이벤트 코드. 6절 목록에 있어야 해요), `trace_id`와 `span_id`(오류 응답의 `trace_id`와 같아요), `role`과 `instance_id`, `resource_kind`와 `resource_id`(작업 대상 자원. 객체나 행이 아니에요), `params`(파라미터. 키는 코드별로 목록에 고정돼 있고 값은 ID, 수, 열거값, 시간뿐이에요), `message`(참고용 영어 한 줄. 기계는 읽지 않아요). 같은 코드가 폭주하면 샘플링해서 줄이고 버린 수를 `log_dropped`로 집계해요. 시크릿과 행의 내용은 어느 필드에도 없고, 테스트가 "출력하는 모든 필드가 목록에 있는가"를 검사해요.
```json
{"ts":"2031-04-02T07:41:12.304Z","severity":"warning","event_code":"storage.connections_exhausted","trace_id":"tr_7c1e","span_id":"sp_02","role":"worker","instance_id":"wk-2","resource_kind":"component","resource_id":"storage","params":{"limit_at":"pool","pool":"worker","in_use":16,"max":16,"waiting":9},"message":"worker storage pool exhausted"}
```
트레이스는 요청 1건, 동기화 1회마다 하나예요. 큐를 거치는 구간은 부모와 자식 관계가 아니라 링크로 이어요. 작업 이력과 실행 이력에 `trace_id`를 남겨 두면, 큐에서 작업을 꺼낸 쪽이 그 트레이스에 이어 붙여요. 속성(attribute)은 역할, 자원의 `id`, 이벤트 코드, 수뿐이에요. 오류와 느린 요청은 모두, 나머지는 샘플링해서 내보내요. 트레이스와 로그는 제품 안에 쌓지 않고 열린 형식으로 내보내요(보관은 수신하는 쪽의 일이에요). 실행별 단계 수와 소요 시간은 제품이 저장해요(ADR-0013).
```
 요청:   api 경로 ─▶ 권한 ─▶ (읽기) engine 질의 ─▶ 스토리지(있으면) ;  (쓰기, 오래 걸림) 작업 생성 ──링크──▶ worker 작업 ─▶ 단계 ─▶ 스토리지
 동기화: worker `sync_run`(루트) ─▶ 수집 ─▶ 정제 ─▶ 매핑 ─▶ 식별 ─▶ 링크 ─▶ 차이 ─▶ 안전장치 ─▶ 적용 ──링크(`change_seq`)──▶ engine 프로젝션 반영
```

### 3.5 프락시움 자신의 목표
ADR-0013 6절의 틀(`threshold`, `period`, `allowed_miss`, 못 지킨 비율, 빠르게 나빠짐)을 그대로 써요. 목표도 정의 항목이라 버전, 제안, 승인 절차를 거쳐요. `target`이 `component`이고 `kind`는 `latency`, `lag`, `on_time_ratio`, `error_ratio`예요(ADR-0013 6절의 고정 목록에 더하는 4종).

| 목표 | 대상 | 기본값(설정) |
|---|---|---|
| 질의 응답 시간 | 엔진, API의 질의 경로 | `impact`는 제품 책임자의 숫자(REQ-142), 그 밖의 사전 정의 질의는 p95 1초 |
| 적용 지연, 프로젝션 지연 | 워커, 엔진 | 스트림의 `apply_lag_s` 60초, `projection_lag_s` 30초 이내(배치는 다음 주기 전) |
| 스케줄의 정시 실행, 자체 측정 | 워커, 합성 점검 | `tick_lag_s` 10초 이내 99%, `total` 60초 이내 99% |

## 4. 엔드투엔드 자체 측정
부품 지표가 모두 정상이어도 사용자가 보는 응답이 막힐 수 있어요. 그래서 제품이 합성 변경을 스스로 흘려 보내고, 사용자와 같은 경로로 결과가 응답에 나타나는지 측정해요(합성 점검, 블랙박스 점검). 부품 지표는 원인을, 이 측정은 증상을 알려 줘요.
- **합성 소스.** 제품에 내장된 소스 `synthetic_probe`예요(`sys` 타입을 시스템 KG에 적용해요). 읽기 전용 어댑터가 정해진 간격(스트림 60초, 배치 15분)으로 가상의 레코드를 발행해요. 어디에도 쓰지 않아요.
- **시스템 KG.** 객체는 운영 KG가 아니라 별도의 시스템 KG에 담기고, 타입은 예약된 네임스페이스 `sys`의 것(`system` 층)이에요. 이 KG는 `sys` 항목만 쓰는 정의 세트를 가리켜요(11절). 서버 1대, 전원 장치 2대, 이중화 그룹 1개(`min_alive` 1), 마커 객체 1개(속성 `probe_seq`)뿐이고 마커의 속성 값만 갱신하므로 객체 수가 늘지 않아요. 운영 KG, `impact`, 헬스 상태, 역량 질문에는 섞이지 않아요. 경로는 `/v1/kgs/{kg}/...`이고 점검용 서비스 계정은 그 KG만 읽어요.
- **측정 방법.** 같은 변경을 4단계에서 확인해요. 마지막 단계는 서비스 계정(`acct_self_probe`, 그 KG만 `viewer`)이 일반 HTTP 요청을 보내서 로그인, 한도, 엔벨로프, 엔진까지 한 번에 거쳐 가요. 추가로 미리 정한 장애 객체(전원 장치 1대)로 `impact`를 질의해 기대 응답(서버 `exposed_direct`)과 비교해요.
- **지표.** `e2e_probe_latency_s`(`detail_key`: `emit_to_ingested` 수집 변경 로그에 기록됨, `ingested_to_applied` `applied_through`가 넘어감, `applied_to_projected` 엔진이 `change_seq`를 반영, `projected_to_answered` API 응답에 나타남, `total`), `e2e_probe_success`(제한 시간 안에 나타났는지), `e2e_probe_age_s`(마지막 성공 이후의 경과 시간), `e2e_probe_answer_correct`.

**실패했을 때 알 수 있는 것.** 막힌 단계가 원인의 첫 단서예요.

| 막힌 단계 | 의심 대상 | 이어지는 진단 |
|---|---|---|
| 수집(1단계) | 수집기, 수집 변경 로그 | `ingest.*`, `change_log.*` |
| 적용(2단계) | 워커, 스토리지, 보류 | `apply.*`, `storage.*` |
| 프로젝션(3단계) | 엔진, 프로젝션으로 가는 변경 경로, 파일 스토어 | `engine.projection_lagging`, `file_store.*` |
| 응답(4단계), 응답이 틀림 | API, 권한 캐시, 엔진 호출 / 영향 범위 계산이나 프로젝션 반영 | `api.*`, `auth.*` / `engine.answer_mismatch` |
| 결과가 오지 않음 | 점검을 실행하는 워커 자체 | `e2e_probe_age_s`를 외부에서 확인해요(12절) |

한계: 점검은 워커의 스케줄로 실행돼서 워커가 모두 멈추면 같이 멈춰요. 그래서 결과가 오지 않는 것 자체가 실패 신호예요. 작은 그래프 하나만 측정하므로 큰 KG의 성능은 측정하지 못하고, 소스 어댑터는 거치지 않아요(소스 측 점검이 맡아요).

## 5. 헬스 상태와 진단
### 5.1 역할별 3가지 점검과 의존 대상
널리 쓰는 헬스 체크처럼 3가지로 나눠요. **live**(프로세스가 살아 있는가)는 의존 대상을 보지 않아요(한 곳의 장애가 모든 인스턴스를 장애로 만들지 않게요). **ready**(요청을 받을 수 있는가)는 그 역할의 주 기능 하나를 기준으로 하고, **started**(기동이 끝났는가)가 참이 되기 전에는 거짓이에요. 응답은 비용이 낮고 로그인이 필요 없고 상세 내용이 없어요. 경로는 `GET /v1/health/live`, `GET /v1/health/ready`, `GET /v1/health/started` 3개예요(자원의 헬스 상태 목록 `/v1/health`와는 다른 것이고 배포 환경의 점검이 써요). ready의 응답은 `{ready, can: {ask, write, ingest, apply}, reasons: [이벤트 코드]}`예요. HTTP 상태는 주 기능만 따라요.

| 역할 | live | ready(주 기능) | started |
|---|---|---|---|
| API | 프로세스가 응답해요 | **질의**를 받을 수 있는지: 권한 캐시가 있고 엔진이나 SQL 경로 중 하나가 정상이에요. 스토리지가 멈춰도 참이에요(`can.write`만 거짓) | 설정 사본과 권한 캐시를 처음 읽어 들였어요 |
| 워커 | 프로세스가 응답하고 리스를 연장하는 루프가 실행 중이에요 | 스토리지에 쓸 수 있고 작업을 획득할 수 있어요 | 스키마 버전이 코드가 기대하는 버전과 일치하고 워커로 등록됐어요 |
| 질의 엔진 | 프로세스가 응답해요 | 프로젝션이 하나라도 로드돼 있어요(스토리지와 무관) | 최초 프로젝션 로드를 마쳤어요 |
| 수집기 | 프로세스가 응답해요 | 본체에 접속해 작업을 받아 소스를 읽을 수 있어요 | 허용된 소스와 소스 커서를 받아 왔어요 |

의존 대상(스토리지, 수집 변경 로그, 파일 스토어, SSO, 알림 채널, 시크릿 제공자)은 역할이 호출한 결과(`dependency_up`)와 가벼운 점검으로 헬스 상태를 따로 산출해요. 부품도 ADR-0013 5절의 6가지 상태와 규칙을 써요. 의존 관계는 제품에 내장돼 있어요: API는 엔진(질의)에 hard, 엔진은 파일 스토어(프로젝션 파일)와 변경 시퀀스에 hard, API는 스토리지(쓰기와 사본 재로드)와 SSO(토큰 확인)에 soft(질의에 한해), 워커의 수집 부분은 마스터 키(`master_key`)에도 hard, 워커는 스토리지, 수집 변경 로그, 파일 스토어에 hard이고 시크릿 제공자, 알림 채널, 소스에 soft, 수집기는 본체에 hard예요.

### 5.2 원인이 상위로 올라가는 예시
가상의 예시예요. 원인은 부품의 의존 관계를 따라 위로 올라가요.
```
 질의 응답 시간 목표가 빠르게 나빠짐     engine    degraded           (자체 검사: 오버레이가 커져 컴팩션이 겹침)
   ◀ engine.projection_lagging             engine    degraded           프로젝션이 12분 뒤처져 변경이 한꺼번에 몰려 들어옴
     ◀ apply.lag_high                      worker    affected_upstream  적용이 지연돼 프로젝션으로 갈 변경이 늦게 도착함
       ◀ storage.connections_exhausted     storage   degraded           ◀ 근본 원인: 워커의 풀 16/16, 대기 9
```
근본 원인 하나에 딸린 알림은 하나로 묶여요(ADR-0013 8.2). 응답이 느린 이유가 밀린 변경을 몰아서 따라잡는 비용이라는 설명은 측정으로 확인할 가정이에요.

### 5.3 `diagnose`
`POST /v1/ops/diagnose`예요. 상태를 바꾸지 않아요(안전함).
- **입력.** `target`(부품 목록, 역할, 또는 전체), `symptom_ref`(알림 `alert`, 목표 `objective`, 이벤트 코드 `event_code` 중 하나. 없으면 지금 이상 상태인 것 전부), `include`(`evidence`, `actions`), `depth`(의존 관계를 몇 단계까지 올라갈지), `limit`, `cursor`.

| 출력 | 설명 |
|---|---|
| `diagnosis_id`, `evaluated_at`, `catalog_version` | 진단의 식별자, 평가 시각, 사용한 규칙 세트의 버전 |
| `served_by`, `persisted` | 응답한 인스턴스, 스토리지에도 저장했는지(저장하지 못하면 그 인스턴스의 메모리에 `ops.diagnosis_ttl_s` 동안만 있어요) |
| `coverage` | 보고한 인스턴스와 보고하지 못한 인스턴스. **보고하지 못한 것은 정상이 아니라 알 수 없음이에요** |
| `findings[]` | 이상 상태인 항목마다: `component`, `state`, `symptom`(무엇이 이상 상태인지) |
| `findings[].suspected_causes[]` | 원인으로 의심되는 항목: `code`, `component`, `via`(의존 경로), `root`, `rule_matched`(`rule_id`, `catalog_version`) |
| `findings[].evidence[]` | 근거가 된 신호: `signal`, `resource`, `observed`, `threshold`, `window`, `at` |
| `findings[].actions[]` | 실행할 수 있는 운영 액션: `action`, `params`, `reason_rule`, `requires_approval`(힌트), `dry_run`(경로) |
| `unknowns[]` | 판정할 신호가 없어 알 수 없는 항목 |

**신뢰도(confidence)는 없어요.** 어떤 규칙이 일치했는지(`rule_matched`)만 있어요. 일치하는 규칙이 없으면 `suspected_causes`가 비고 `unknown_cause: true`예요(원인을 만들어 내지 않아요). 규칙은 6절의 이벤트 코드 항목(`common_causes`, `look_at`, `actions`)에서 나온 고정 조건이고, 같은 신호면 같은 진단이 나와요.

### 5.4 스토리지 없이 답하는 범위
프락시움이 비정상일 때도 프락시움에 질의할 수 있어야 해서, 기본적으로 역할이 메모리에 보유한 정보로 답해요(ADR-0011).

| 스토리지 없이 되는 것 | 스토리지가 있어야 하는 것 |
|---|---|
| `live`, `ready`, `started`, 역할이 보유한 최근 신호(링 버퍼. 길이는 14절)와 의존 대상의 헬스 상태 | 과거 추이(`metric_point`), 타임라인, 알림 목록 |
| `diagnose`(`persisted: false`), 이벤트 코드 목록(제품에 내장돼 있어요), 액션 목록 | 감사 로그와 실행 이력의 기록, 설정 변경(마지막 사본은 읽을 수 있어요) |
| 스토리지를 쓰지 않는 미리 실행, 긴급 액션 3종(아래) | 나머지 운영 액션의 실행(감사 로그를 남길 수 없어서예요) |

- **신호를 수집하는 방법.** 역할마다 자기 헬스 상태와 지표를 노출하는 엔드포인트가 있고, API 역할이 다른 인스턴스에 직접 질의해 모아요(인스턴스 목록은 하트비트로 메모리에 보유해요). 접속하지 못한 인스턴스는 응답에 「알 수 없음」으로 표시돼요.
- **감사 로그를 쓸 수 없을 때.** 상태를 바꾸는 운영 액션은 거절해요(기록 없이 실행하는 액션은 없어요). 예외는 스토리지 없이도 가능한 긴급 액션 3종(`pause_source`, `pause_sources_by_secret`, `enter_maintenance_mode`)이에요. 이 액션은 역할의 디스크에 먼저 기록해 두었다가 스토리지가 복구되면 감사 로그로 옮겨요. 디스크에도 기록하지 못하면 거절해요.
- **엔진이 새로 기동할 때.** 로드할 프로젝션이 파일 스토어에 없으면 스토리지에서 직접 구축해 로드해요. 파일 스토어는 빠른 기동을 위한 것이고 필수가 아니에요. 스토리지에도 파일 스토어에도 없으면 `started`가 거짓으로 남아요.
- **장기 장애 시 권한 캐시의 경과 시간.** 스토리지 없이 답하는 동안 권한 캐시의 경과 시간이 상한(15분)을 넘으면 기본 부여된 읽기만 답해요(`restricted` 이상은 거절). 그래서 장애가 길어지면 질의 가능 범위가 줄어요. `grant_cache_age_s`가 상한에 가까워지면 알려요.

## 6. 이벤트 코드와 오류 코드의 목록
로그의 이벤트 코드, API의 오류 코드, 알림 종류, 헬스 상태의 원인을 **하나의 목록**으로 제품에 내장해요. 기계가 읽는 파일이고 버전이 있으며 **계약의 일부**예요. 사람용 절차서의 내용은 이 목록에서 생성하고 따로 손으로 쓰지 않아요.

**이름 규칙.** `<그룹>.<세부>`, 소문자 snake_case예요. 요청 오류의 그룹은 REST의 것(`invalid_input`, `not_found`, `permission`, `conflict`, `stale`, `rate_limited`, `blocked`)을 그대로 써요. 시스템의 그룹은 `api`, `auth`, `worker`, `engine`, `collector`, `storage`, `change_log`, `file_store`, `sso`, `notify`, `ingest`, `apply`, `probe`, `config`, `rollout`, `secret`, `ops`예요. 시크릿 관련 코드(`secret.master_key_unreachable`, `secret.expiring`, `secret.unwrap_failed`)도 같은 목록에 있어요(ADR-0018). 한번 발행한 코드의 의미는 바꾸지 않고, 추가는 같은 메이저 버전 안에서 할 수 있고, 삭제할 때는 `deprecated`로 남겨요.

| 필드 | 설명 |
|---|---|
| `code`, `kinds` | 이름, 사용처(`event` 로그 / `error` API 오류 / `alert` 알림 종류 / `cause` 헬스 상태의 원인. 여러 개일 수 있어요) |
| `severity`, `alert_severity`, `meaning`, `params` | 로그의 심각도, 알림이 되면 `info` / `warning` / `critical`, 의미 한 줄(영어. 화면의 문구는 코드로 만들어요), 파라미터의 키와 타입 |
| `common_causes`, `look_at` | 흔한 원인(다른 `cause` 코드), 확인할 항목(신호, 자원, 기대하는 값) |
| `actions`, `since`, `status` | 실행할 수 있는 운영 액션(이름, 조건, 되돌리는 액션), 처음 도입된 목록 버전, `active` / `deprecated` |

**버전과 경로.** 목록의 버전은 `catalog_version`이에요(코드를 추가하면 마이너 번호, 필드 구조를 바꾸면 메이저 번호가 올라가요). 역할은 자기가 쓰는 버전을 `role_version_info`로 내보내요. `GET /v1/ops/event-codes`가 목록을, `GET /v1/problems/{code}`가 오류 코드의 설명을 반환해요(`storage.*`와 `secret.version_check_failed`도 이 목록에 있어요). 테스트가 3가지를 검사해요: 출력하는 모든 코드가 목록에 있는지, `actions`가 7절의 목록 안에 있는지, `look_at`의 지표가 지표 목록에 있는지예요.

가상의 항목 4개예요. 첫 번째는 파일 형식 그대로, 나머지는 같은 필드를 표로 적었어요.
```yaml
- code: storage.connections_exhausted
  kinds: [event, cause, alert]
  severity: warning
  alert_severity: critical
  meaning: connections to the storage are used up
  params: {limit_at: "pool|server", pool: "api|worker|engine", in_use: int, max: int, waiting: int}
  common_causes: [worker.candidate_runs_hold_connections, worker.pool_oversized]
  look_at: [{signal: db_pool_in_use, resource: worker, want: "max와 같다"}, {signal: db_pool_waiting, resource: worker, want: "0보다 크다"}]
  actions: [{action: update_setting, when: "limit_at == pool", hint: "worker.candidate_run_concurrency를 줄인다", reverse: revert_setting}, {action: drain_worker, when: "한 인스턴스가 대부분을 사용", reverse: undrain_worker}]
```
| 코드, 종류, 심각도 | 파라미터 | 흔한 원인 | 확인할 항목 | 실행할 수 있는 운영 액션 |
|---|---|---|---|---|
| `apply.lag_high` · `cause`, `alert` · warning, 알림 warning | `lag_seq`, `lag_s` | `storage.connections_exhausted`, `worker.all_drained`, `ingest.stream_held` | `apply_lag_seq`(증가 중), `oldest_job_age_s`(`stream_apply`, 길어짐), `held_streams`(0이면 보류가 원인이 아님) | `release_held_stream`(`held_streams` > 0일 때), `undrain_worker`(모든 인스턴스가 드레인됐을 때) |
| `engine.projection_lagging` · `event`, `cause` · warning | `lag_seq`, `lag_s`, `instance` | `apply.lag_high`, `file_store.unreachable`, `engine.compaction_slow` | `projection_lag_seq`(증가 중), `engine_compaction_duration_s`(길어짐) | `rebuild_projection`(1800초 넘게 뒤처지고 상류가 정상일 때), `roll_engine`(한 인스턴스만 뒤처질 때) |
| `stale.dry_run_outdated` · `error` · info | `dry_run_id`, `changed`(`target`, `setting_rev`, `risk`) | 미리 실행한 뒤 대상이 바뀜 | `ops_dry_run_action`의 `expires_at` | 미리 실행을 다시 수행해요 |

## 7. 운영 액션
프락시움 운영 작업은 셸이나 DB에 직접 접속해서 하지 않아요. 모두 정해진 목록의 액션이고, API로 제공하며 명령줄과 MCP로도 노출해요. 액션은 프락시움 자신의 상태만 바꿔요. 소스와 현실 시스템에는 읽기만 해요(ADR-0001).

### 7.1 목록
위험 축은 ADR-0007의 4가지예요: 상태 변경 여부 · `reversibility` · `disruption` · 사람 손(모든 운영 액션이 「아니요」라 표에서 뺐어요). 「소스」 범위의 액션은 그 소스를 범위에 가진 `operator`가 수행해요.

| 액션 | 동작 | 축 | 선언한 효과 | 되돌리는 액션 | 필요한 역할 | 승인 기본값 |
|---|---|---|---|---|---|---|
| `pause_source`, `pause_feed`, `pause_stream` | 소스, 피드, 스트림을 일시 중지해요 | 예 · reversible · none | 새 수집이 중단되고 `paused`로 표시돼요. 신선도는 나빠지지만 질의는 그대로예요 | `resume_source`, `resume_feed`, `resume_stream`(스케줄이 자동으로 멈춘 소스도 재개해요) | `operator`(소스) | 아니요 |
| `release_held_stream` | 보류된 스트림에 쌓인 변경을 수신 순서대로 적용 | 예 · irreversible · none | `held`가 해제되고 새 KG 버전이 생성돼요 | 없음(새 변경으로 앞으로 수정해요) | `operator`(소스) | 예 |
| `discard_held_stream` | 쌓인 변경을 버리고 전체를 다시 수집해 정합성 대조를 해요(대조할 수 있는 소스만) | 예 · irreversible · data_loss | 쌓인 변경이 사라지고 정합성 대조가 실행돼요 | 없음 | `operator`(소스) | 예 |
| `rerun_sync`, `cancel_sync` | 실행을 다시 하기, 실행 중인 작업을 취소 | 예 · reversible · none | 새 실행이 시작되거나 중단돼요. 안전장치는 그대로예요 | 서로 | `operator`(소스) | 아니요 |
| `drain_worker` | 워커 인스턴스 하나가 새 작업은 받지 않고 진행 중인 작업만 끝내요 | 예 · reversible · none | 그 인스턴스가 `drained`가 되고 큐는 다른 인스턴스가 가져가요 | `undrain_worker` | `operator`(전체). 전부 드레인은 `admin` | 아니요(범위가 넓으면 예) |
| `rebuild_projection` | 프로젝션을 워커가 다시 구축하고 엔진이 교체해요 | 예 · reversible · none | 그동안 기존 프로젝션으로 답해요. 메모리가 2배 필요해요 | 필요 없음(파생 데이터) | `operator`(전체) | 아니요 |
| `roll_engine` | 엔진을 인스턴스 하나씩 내렸다가 다시 올려요(기동이 끝나면 다음 인스턴스) | 예 · reversible · none | 인스턴스마다 `started`를 거쳐요 | `roll_engine` | `operator`(전체) | 아니요 |
| `pin_serving`, `unpin_serving` | 질의를 이전 KG 버전에 고정하고 해제해요(`pending` → `active`) | 예 · reversible · none | 응답의 `as_of.pinned`에 표시돼요 | 서로 | `operator`(전체), `admin` | 아니요 |
| `run_archive_now`, `run_purge_now` | 보존 기한이 지난 데이터를 콜드 스토리지로 아카이브 / 삭제 | 예 · reversible · none / irreversible · data_loss | 현재 스토리지의 크기가 줄어요(아카이브한 데이터도 읽을 수 있어요) / 삭제한 만큼 줄어요 | 필요 없음 / 없음 | `operator`(전체) / `admin` | 아니요 / 예 |
| 스토리지 액션(부하 감소, 보류, 백필 중지, 장기 트랜잭션 종료, 질의 패턴 차단, 테이블 정리) | 스토리지 신호가 가리킬 때 PostgreSQL을 관리해요. 이름과 축은 구현하는 변경이 목록에 추가해요 | 예 · 액션마다 | 신호(`storage.*`)에 나타나요 | 액션마다 | `operator`(전체), 범위가 넓으면 `admin` | 위험 축을 따라요 |
| `run_connection_check_now` | 소스에 접속해 보고 결과만 기록(소스 보호 장치를 거쳐요) | 아니요 · reversible · none | 소스의 연결 헬스 상태가 새로 산출돼요 | 필요 없음 | `operator`(소스) | 아니요 |
| `pause_sources_by_secret` | 시크릿이 유출됐다고 의심될 때 그 값을 쓰는 소스를 한 번에 중지하고 값에 `suspected`를 표시해요 | 예 · reversible · none(수집만 중지하고 질의는 가능해요. 사람 손은 필요 없어요) | 그 값을 쓰는 소스가 모두 `paused`가 되고 새 연결이 없어요 | `resume_source`(값의 이전 버전으로 되돌린 뒤 소스마다) | `operator`(그 소스들을 모두 범위에 가진), 범위가 넓으면 `admin` | 아니요(되돌릴 수 있는 보호 액션이에요. 설정으로 켤 수 있어요) |
| `rotate_system_token` | 서비스 계정의 토큰을 교체해요 | 예 · irreversible · service_affecting | 기존 토큰은 병행 사용 기간이 끝나면 차단돼 그 토큰을 쓰던 시스템의 접속이 끊겨요. 스토리지가 정상이면 회수는 30초 안에 전파돼요 | 없음 | `admin` | 예 |
| `rewrap_master_key` | 데이터 키를 새 마스터 키로 다시 래핑해요(ADR-0018) | 예 · reversible · none | 기존 마스터 키를 폐기해도 되는 시점이 와요. 값은 다시 암호화하지 않아요 | 기존 마스터 키로 다시 래핑해요 | `admin` | 예 |
| `redact_raw_record` | 원본 레코드의 내용을 제거해요(해시, 위치, 이유만 남겨요) | 예 · irreversible · data_loss | 그 레코드를 다시 처리하면 값은 제거된 것으로 취급해요 | 없음 | `admin` | 예 |
| `enter_maintenance_mode`, `exit_maintenance_mode` | 수정 요청(정의 수정, 제안, 병합)을 받지 않기 / 수정 요청 수신 재개. 질의와 수집·적용은 그대로 | 예 · reversible · service_affecting / none | 수정 요청이 `blocked.maintenance_mode`로 차단돼요 / 해제돼요 | 서로 | `admin` | 예 / 아니요 |
| `update_setting`, `revert_setting` | 설정을 변경하고 되돌려요(8절) | 예 · reversible · none | 새 버전이 만들어지고 역할이 다음에 읽을 때 적용해요 | 서로 | `admin` | 설정 항목의 위험 축을 따라요 |

토큰 값과 시크릿은 어느 액션의 결과에도 포함되지 않아요. 스토리지 액션은 알파에서 이름과 위험 축만 계약으로 두고, 구현은 클로즈드 베타에서 오픈 베타 사이에 해요(13절). `pause_sources_by_secret`은 값의 버전을 되돌리고 값을 교체하는 일, 복호화 사용 이력을 조회하는 일(ADR-0018)에 앞서 하는 첫 조치예요. 새 토큰은 계정을 관리하는 `admin`의 경로로 발급해요(access-control 11절).

### 7.2 미리 실행
상태를 바꾸는 액션은 실제로 실행하지 않고 무엇이 일어날지만 응답해요(`POST /v1/ops/actions/{action}:dry-run`, 안전함). 응답은 계산한 영향을 선언한 효과와 나란히 적고, 선언한 축보다 무거운 영향이 계산되면 무거운 쪽을 따라요(`raised_by`에 이유). 마지막 인스턴스 하나를 드레인하거나 소스가 소스 커서 위치부터 다시 보낼 수 없는 스트림을 중지하면 위험 축이 올라 승인이 필요해져요. 필드: `dry_run_id`와 `expires_at`(`ops.dry_run_ttl_s`. 실행을 생성할 때까지의 유효 시간이에요. 그 사이 대상이 바뀌면 `stale.dry_run_outdated`로 거절하고, 승인을 기다리는 동안에는 대상이 바뀌었는지만 다시 확인해요), `would_do`(실행할 단계), `effects`(`declared` 선언한 효과, `computed` 무엇이 중단되고 무엇이 영향받는지 집계한 값), `risk`와 `raised_by`, `blocked_by`(중복 실행, 점검 모드, 권한, 범위 초과), `requires`(필요한 역할, 승인 필요 여부와 이유), `reverse_action`, `verification_plan`(사후 확인에서 볼 신호와 기대값).

### 7.3 범위가 너무 넓은 액션을 제한하는 규칙
- 대상이 「전부」이거나 대상의 비율이 `ops.max_targets_ratio`(기본 25%)를 넘으면 `admin`만 실행할 수 있고 승인이 필요해요(워커 전체 드레인, 모든 소스 중지).
- 한 대상에는 한 번에 하나의 액션만 실행돼요. 같은 계열의 액션이 실행 중이면 `conflict.action_in_progress`예요. 생성 요청은 `Idempotency-Key`를 받아요(같은 키와 같은 본문이면 처음 실행을 반환해요). 점검 모드에서 변경 액션(`update_setting` 등)은 `admin`만 실행할 수 있어요.

### 7.4 실행 흐름과 사후 확인
`POST /v1/ops/actions/{action}/runs`가 실행 1건(`action_run`)을 생성해요. 오래 걸리면 작업 자원(`/v1/jobs`)이 함께 생겨요.
```
 requested ─▶ awaiting_approval ─▶ approved ─▶ running ─▶ verifying ─▶ succeeded
                │      └▶ rejected / expired        └▶ failed / canceled   └▶ effect_not_confirmed
                └(승인이 필요 없으면 바로 approved)
```
`action_run`의 필드: `run_id`, `action`, `params`, `target`, `requested_by`(`actor_kind`, `actor_id`), `diagnosis_id`와 그 요약(`rule_id`와 근거 참조. 진단이 사라져도 이력이 남도록 복사해요), `dry_run_id`, `approval`(`required`, `reason`, `approved_by`, `at`), `idempotency_key`, `state`, `result`(`outcome`, 오류 `code`), `verification`, `reverse_of`, `trace_id`.

**사후 확인.** 선언한 효과를 신호에 대한 기대값으로 바꾼 것이 `verification_plan`이에요. 실행이 끝나고 `ops.verification_settle_s`(14절)가 지나면 신호를 확인하고 판정을 결과에 기록해요: `confirmed`(기대한 변화가 나타났어요), `not_observed`(기한 안에 나타나지 않았어요), `contradicted`(반대로 악화됐어요), `unknown`(신호가 없어 판정하지 못했어요). `confirmed`가 아니면 상태는 `effect_not_confirmed`이고 알림이 가요. **자동으로 되돌리지 않아요.** 되돌리는 액션을 제안으로 붙일 뿐이고, 실행은 외부 에이전트나 사람이 해요.

## 8. 설정은 데이터
운영 설정(한도, 임계값, 보존 기한, 동시 실행 수)은 파일을 수정하고 재기동해서 바꾸는 값이 아니라 API로 바꾸는 버전 관리 값이에요. 스토리지 접속 정보처럼 설정을 읽기 전에 필요한 값은 이 목록에 없고 배포 환경이 제공해요. 필드: `key`(점으로 이은 snake_case. `worker.candidate_run_concurrency`), `type`(`int`, `duration`, `ratio`, `bool`, `enum`), `unit`, `min`, `max`, `default`(범위 밖은 `invalid_input.field_range`로 거절해요), `applies`(`live` 역할이 다음에 읽을 때 적용 / `restart_required` 재기동해야 적용), `risk`(변경 시 위험 축. 기본 reversible · none), `rev`, `value`, `previous_value`, `changed_by`, `changed_at`, `reason`, `run_id`, `diagnosis_id`.
- **변경 흐름.** `update_setting` 하나가 유일한 경로예요. `PATCH /v1/settings`도 그 축약 형태라 같은 미리 실행, 승인, 감사를 거쳐요. 미리 실행은 범위 검사, 설정을 쓰는 역할과 인스턴스, 재기동할 인스턴스의 목록을 보여요. `If-Match`가 직전 버전과 일치할 때만 새 버전이 만들어져요.
- **리버트.** `revert_setting`은 직전 버전의 값으로 새 버전을 만들어요(삭제하거나 되감지 않아요).
- **재기동해야 적용되는 설정.** 새 버전이 만들어져도 인스턴스마다 `setting_rev_applied`가 올라가야 적용돼요. 그때까지 `pending_restart`로 롤아웃 상태(12.2)에 표시돼요.
- **정의와의 경계.** 소스 보호 값, 안전장치 값, 알림 규칙, 목표는 정의 항목이라 브랜치와 제안으로 수정해요(ADR-0014). 이 절의 설정은 제품 운영에 관한 것이고 정의가 아니에요. 보존 기한(지표, 감사 로그 등)도 여기에 포함돼요. 기본값은 [기본값 표](defaults.md)에 있어요.
- 예(기본값(설정)): `worker.candidate_run_concurrency`(2, `live`), `worker.db_pool_size`(16, `restart_required`), `ops.dry_run_ttl_s`(600), `ops.max_targets_ratio`(0.25), 액션마다 `ops.action.<액션>.require_approval`과 `.delegation`(9.2).

## 9. 권한과 기록
### 9.1 역할과 범위
ADR-0012의 역할과 범위를 그대로 써요. 읽기에도 새 역할이 없어요.

| 하는 일 | 역할과 범위 |
|---|---|
| 부품의 헬스 상태, 신호, `diagnose`, 이벤트 코드 목록 읽기 | 전체 범위의 `operator`, `admin`, `auditor`(부품은 데이터의 타입 범위로 구분할 수 없어요). `live`, `ready`, `started`의 단순 응답은 로그인 없이 |
| 소스 단위 액션(중지, 재개, 재실행, 연결 점검, 보류된 스트림) | 그 소스를 범위에 가진 `operator` |
| 제품 전체 대상 액션(프로젝션, 엔진, 고정, 아카이브, 워커 인스턴스 하나) | 전체 범위의 `operator`, `admin` |
| 범위가 넓은 액션, 설정, 점검 모드, 삭제, 토큰 | `admin` |
| 승인 | 요청한 쪽이 아닌 `admin`이나 범위를 가진 `operator`인 **사람**. 서비스 계정은 승인하지 못해요 |

### 9.2 서비스 계정(에이전트)과 승인의 설정
에이전트는 서비스 계정이에요(예: `acct_ops_agent`). 사람과 같은 역할과 범위를 받고, 토큰은 해시로만 저장되고, 만료, 교체, 호출 한도가 있어요(ADR-0012). 계정의 `contacts`가 한도 초과와 이상 사용 알림을 받아요. 에이전트는 승인할 수 없어요. 계정을 비활성화하거나 토큰을 회수하면 스토리지가 정상일 때 30초 안에 전파돼요(권한 캐시 경과 시간의 상한 15분이 최대 지연이에요). 서비스 계정의 `on_behalf_of`는 관리자가 그 계정에 연결해 둔 사람 중에서만 선택해요. 액션마다 설정이 2가지 있어요.
- `require_approval`: 승인이 있어야 실행해요. 기본값은 위험 축에서 정해져요. 되돌릴 수 없거나(`irreversible`) 서비스가 끊기는(`service_affecting`, `data_loss`) 액션은 참이에요. 요청한 주체가 사람이든 서비스 계정이든 같아요.
- `delegation`: 서비스 계정이 그 액션을 어디까지 수행할 수 있는지. `none`(수행 불가, 기본값) / `with_approval`(요청하고 사람이 승인해요) / `autonomous`(승인 없이 실행해요). `autonomous`는 되돌릴 수 있고 서비스가 끊기지 않는 액션에만 설정할 수 있어요. 아니면 `invalid_input.autonomy_exceeds_risk`로 거절해요.

### 9.3 감사 로그
운영 액션은 모두 감사 로그(ADR-0012)에 기록돼요. 읽기(신호, `diagnose`)는 일반 읽기라 기록하지 않아요. `action`은 `ops_action_requested`, `ops_action_approved`, `ops_action_rejected`, `ops_action_finished`, `setting_changed`이고, 기존 필드(`actor_kind`, `actor_id`, `target_*`, `outcome`, `request_id`)에 다음 필드를 추가해요: `ops_run_id`, `diagnosis_id`와 `diagnosis_rule_ids`(어떤 진단의 어느 규칙이 일치해서 근거로 삼았는지. 사람이 근거 없이 실행한 경우는 비어 있고, 서비스 계정의 요청에서 비어 있으면 `invalid_input.diagnosis_required`), `dry_run_id`, `verification_verdict`. 어느 역할의 어떤 실행이 시크릿을 언제 복호화해 사용했는지(복호화 사용 이력)는 ADR-0018의 감사 필드이고, `secret_unwrap_count`가 그 횟수를 집계해요.
```json
{"audit_id":"au_9d1","occurred_at":"2031-04-02T07:59:30Z","actor_kind":"system_account","actor_id":"acct_ops_agent","action":"ops_action_finished","target_kind":"setting","target_id":"worker.candidate_run_concurrency","outcome":"allowed","ops_run_id":"ar_204","diagnosis_id":"dg_5521","diagnosis_rule_ids":["r_storage_conn_01"],"verification_verdict":"confirmed"}
```

## 10. 에이전트용 인터페이스
### 10.1 운영의 MCP 인터페이스
KG를 읽는 MCP(사전 정의 질의 13종 등)와 **별도로** 제공하는 인터페이스예요. 둘은 같은 유스케이스 위의 어댑터라는 규칙(ADR-0016)을 따르고 권한 판단과 오류 코드가 같아요. KG의 MCP는 읽기만 하고 이쪽에는 액션 툴이 있어요. 모두 MCP 게이트웨이에 등록되는 툴이에요. **프락시움 내부에는 진단을 보고 액션을 골라 실행하는 루프를 두지 않아요.** 운영은 외부 에이전트가 해요. 이미 내장된 고정 안전장치(리스 인계, 연속 실패 시 스케줄 중지)는 그대로 유지돼요.

**운영 액션 하나가 MCP 툴 하나예요.** 액션 이름을 인자로 받는 툴 하나로 합치지 않아요. 툴 허용 목록만 지원하는 게이트웨이에서도 액션별로 차단하고 허용할 수 있게 하려는 거예요. 툴 이름은 `ops_` 뒤에 7.1절의 액션 이름을 붙여요(예: `ops_drain_worker`, `ops_update_setting`, `ops_pause_sources_by_secret`). 인자는 `params`, `dry_run_id`, `diagnosis_id`이고 승인이 필요하면 `awaiting_approval` 상태로 반환돼요.

| 툴 | 종류 | 하는 일 |
|---|---|---|
| `ops_get_signals`, `ops_list_components`, `ops_get_component_health` | 읽기 | 부품의 신호(`/v1/metrics/query`의 `component`)와 헬스 상태(`/v1/health`) |
| `ops_diagnose`, `ops_list_event_codes`, `ops_get_event_code` | 읽기 | 5.3의 진단, 6절의 목록 |
| `ops_list_actions`, `ops_get_settings`, `ops_get_rollout`, `ops_get_synthetic_checks`, `ops_list_action_runs`, `ops_get_action_run` | 읽기 | 액션 목록, 설정, 롤아웃 상태, 자체 측정 결과, 실행과 사후 확인 |
| `ops_dry_run_action` | 읽기(안전함) | 7.2. 액션 이름을 받아 무엇이 일어날지만 응답해요 |
| `ops_<액션>` | **액션** | 7.1의 액션마다 툴이 하나씩 있어요. 실행을 생성해요 |

승인과 거절 툴은 없어요. 사람이 API(`:approve`)와 명령줄로 해요. 응답 크기의 상한과 다음에 할 일을 알려 주는 오류는 KG의 MCP와 같아요(ADR-0010).

### 10.2 끝에서 끝까지 따라가는 가상의 예시
설정은 2단계(제안과 승인, 13절)이고 `update_setting`에 `with_approval`이 적용돼 있어요.

**1. 알림.** 에이전트가 알림 `a_311`을 받아요: 엔진의 질의 응답 시간 목표가 빠르게 나빠짐(p95 2.4초, 목표 1초).

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
에이전트의 해석: 질의가 느린 것은 엔진의 프로젝션이 12분 뒤처졌기 때문이고, 워커가 스토리지 연결이 가득 차서(풀 16개 중 16개를 사용 중이고 드라이 런 2건이 연결을 사용 중이에요) 적용하지 못해요.

**3. 미리 실행.** `ops_dry_run_action(update_setting, {key: worker.candidate_run_concurrency, value: 1}, diagnosis_id: dg_5521)`
```yaml
dry_run_id: dr_88   expires_at: "2031-04-02T07:56:00Z"
would_do: ["설정 버전 14 → 15", "wk-1, wk-2, wk-3이 다음 읽기(30초 이내)에 새 값을 적용해요"]
effects: {declared: "새 드라이 런의 동시 실행 수가 줄어 워커의 연결 사용량이 내려가요", computed: {running_candidate_runs: 6, stopped_now: 0, queued_after: 4}}
risk: {mutates: true, reversibility: reversible, disruption: none, physical: false}   raised_by: []
requires: {role: admin, approval: true, reason: "ops.action.update_setting.require_approval"}
reverse_action: {action: revert_setting, params: {key: worker.candidate_run_concurrency, to_rev: 14}}
verification_plan: [{signal: db_pool_in_use, resource: wk-2, want: "< 13", within_s: 600}, {signal: apply_lag_seq, resource: worker, want: "줄어든다", within_s: 900}]
```

**4. 요청과 승인.** `ops_update_setting(..., dry_run_id: dr_88, diagnosis_id: dg_5521)`가 `{run_id: ar_204, state: awaiting_approval}`를 반환해요. 승인 요청이 `admin`에게 전달되고 미리 실행과 진단의 요약이 첨부돼요. 사람이 읽고 `POST /v1/ops/action-runs/ar_204:approve`로 승인해요. 에이전트는 기다리면서 상태 변경을 이벤트 채널로 수신해요.

**5. 실행.** 설정 버전 15가 만들어지고 워커 3대가 새 값을 읽어요(`running`). 진행 중이던 드라이 런 6건은 끝날 때까지 기다려요.

**6. 사후 확인.** 9분 뒤에 신호를 확인해요: `verification: {verdict: confirmed, observed: [{signal: db_pool_in_use, resource: wk-2, value: 9}, {signal: apply_lag_seq, value: 3100, trend: "줄어든다"}]}`. `ar_204`는 `succeeded`가 되고 알림 `a_311`은 조건이 해소돼(연속 3회) `resolved`가 돼요.

**7. 기록.** 감사 로그 `au_9d1`(9.3)에 누가(`acct_ops_agent`가 요청, 사람이 승인), 무엇을, 어떤 진단과 규칙(`dg_5521`, `r_storage_conn_01`)을 근거로 했는지, 결과(`confirmed`)가 남아요. `confirmed`가 아니었다면 에이전트는 `reverse_action`을 같은 방식으로 요청했을 거예요.

## 11. 프락시움도 KG의 객체
제품에 내장된 소스 `praxium_self`가 프락시움의 구성을 **시스템 KG**에 기록해요. 타입은 예약된 네 번째 네임스페이스 `sys`의 것이고 층은 고정 목록의 10번째 값 `system`이에요. `sys` 타입은 표준 모델의 데이터 파일과 별개로 제품에 내장돼 있고, 시스템 KG는 `sys` 항목만 쓰는 정의 세트를 가리켜요(정의의 메인 브랜치는 배포당 하나예요). 자체 측정(4절)의 가상 객체도 이 KG에 담겨요. 담기는 것은 프락시움 자신(역할 인스턴스, 스토리지, 수집 변경 로그, 파일 스토어, SSO)과 의존 관계(`hard` / `soft`)예요. 인스턴스의 존재는 역할 등록에서(`observed`), 의존 관계는 제품에 내장된 테이블에서(`declared`) 가져와요. 상태가 아니라 구성이에요.

`admin`이 원하면 이 구성을 운영 KG에도 내보내 인프라와 연결할 수 있어요(설정 `ops.self_kg.export_to_main`, 기본 꺼짐. 구현은 클로즈드 베타예요). 연결하면 「이 스토리지에 장애가 나면 무엇이 멈추는가」를 `impact`로 질의할 수 있어요. 계획과 설계 검토에 쓰는 용도예요.

**이 KG에 의존해 프락시움을 감시하지 않아요.** 프락시움에 장애가 나면 그 KG에도 질의할 수 없어요. 구성은 수집해서 구축하는 데 시간이 걸려 현재의 장애를 따라가지 못해요. 같은 파이프라인을 쓰므로 파이프라인이 이상 상태이면 이 KG도 함께 낡아요. 감시 방법은 5.4절과 외부로 내보내는 신호예요.

## 12. 감시 대상 감시, 롤아웃, 증가 추세
### 12.1 감시 대상 감시
신호를 외부로 내보내는 경로와 알림을 보내는 경로에 장애가 나면 아무도 모른 채 정상처럼 보여요. 그래서 정해진 간격(`ops.heartbeat_interval_s`, 기본 60초)으로 항상 하트비트를 두 경로 끝까지 보내요. 지표는 `export_last_success_age_s`, `export_queue_depth`, `export_dropped`(전송하지 못해 버린 수), `alert_heartbeat_age_s`와 `health_evaluator_age_s`(13절), `e2e_probe_age_s`예요. 내보내기가 서비스를 막지 않아요. 크기가 정해진 버퍼가 가득 차면 오래된 것부터 버리고 `export_dropped`로 집계해요. **외부에서 할 일:** 신호가 오지 않는다는 사실을 외부에서 알아야 한다는 것이 요점이라, 조직의 관측 도구가 위 경과 시간이 간격의 3배(기본값)를 넘거나 하트비트가 끊긴 것을 감지해서 제품과 별개로 알려야 해요.

### 12.2 롤아웃 상태
`GET /v1/ops/rollout`이 반환해요. 인접한 두 버전의 역할은 함께 실행될 수 있어야 해요. 스키마 마이그레이션은 확장(새 것 추가), 이전, 축소(옛 것 삭제) 순서로 하고 축소는 다음 버전에서 해요. 테이블 전체를 다시 쓰는 변경은 하지 않고 잠금 대기 한도를 첫 마이그레이션부터 적용해요. 표준 모델과 `sys` 타입도 마이그레이션 단계의 하나로 스토리지에 적재되므로 인스턴스마다 메인 브랜치의 해시가 달라지지 않아요. 적용 리스에는 실행기의 의미 버전이 들어 있어서 낮은 버전의 워커는 적용 리스를 획득하지 못해요. 시점 복구를 하면 KG의 세대 번호가 올라가고 `as_of`, 커서, 이벤트 ID, `min_seq`에 포함돼요(다르면 `conflict.generation_changed`). 역할 간 버전이 `ops.version_skew_alert_s`(기본 3600초) 넘게 불일치하면 `rollout.version_skew_long`으로 알려요. 활성화된 기능은 `/v1/capabilities`의 `features`와 같은 것이에요.
```yaml
schema: {current: 41, target: 42, state: migrating, step: "3/5"}
roles: [{role: api, instance_id: api-1, version: "0.9.2", catalog_version: "1.4.0", api_version: v1, state: ready}, {role: worker, instance_id: wk-1, version: "0.9.1", state: draining}]
skew: {versions: ["0.9.1", "0.9.2"], skewed: true, since: "2031-04-02T06:10:00Z"}
features: [{feature: self_kg, enabled: false}]   settings_pending_restart: [{key: worker.db_pool_size, rev: 9, instances: [wk-2]}]
```

### 12.3 증가 추세 예측
스토리지 크기, 큐 길이, 메모리는 정해진 방법으로 예측해요: 최근 구간(기본 14일)의 일별 최댓값에 최소제곱 직선을 적합하고, 기울기가 양수이면 `time_to_limit_s` = (한도 − 현재값) ÷ 기울기예요. 같은 입력이면 같은 값이 나와요. 한도는 배포 환경이 알려 주는 설정이고 알 수 없으면 예측하지 않아요(`unknown`). 예: 스토리지가 하루 12GB씩 늘고 한도가 600GB인데 현재 480GB이면 10일이고 `storage.growth_limit_near`를 알려요(임계값은 14절). 한계: 계단식 증가와 요일별 패턴은 따라가지 못해요.

## 13. 단계
위임 단계는 3개이고, 제품 단계는 4개(알파, 클로즈드 베타, 오픈 베타, 정식)예요. 알파는 둘로 나뉘어요. 알파 1(뼈대와 최소 경로)에는 이 문서의 기능이 없고 임시 콘솔만 있어요. 알파 2에서 신호, 핵심 진단, 액션, 설정, 감사를 API와 명령줄로 쓸 수 있어요. 임시 콘솔도 프락시움 자신의 진단을 보여요(공개된 API만 써서요).

**알파에서는 계약만 두고 구현은 클로즈드 베타로 미루는 것**: 목표의 소진율과 「빠르게 나빠짐」, 운영 MCP 인터페이스와 `diagnose`의 규칙 세트 전부(알파는 핵심 규칙만), 시스템 KG를 운영 KG에 내보내기. 필드, 경로, 코드 목록은 알파부터 있어요. 스토리지 신호는 이름과 단위가 알파부터 계약이고 구현은 단계별이에요. 스토리지 액션은 클로즈드 베타에서 오픈 베타예요. 한도까지 남은 시간의 예측(12.3)은 오픈 베타예요. 시크릿 쪽은 복호화 사용 이력과 만료 알림이 클로즈드 베타, 마스터 키의 헬스 상태, 필드 암호화 점검, 긴급 액션이 오픈 베타예요(ADR-0018).
| 단계 | 에이전트가 하는 일 | 설정 | 사람이 하는 일 |
|---|---|---|---|
| 1. 보기와 진단 | 신호를 읽고 `diagnose`를 실행해요. 액션은 실행하지 못해요 | 모든 액션이 `delegation: none` | 진단을 보고 직접 실행해요 |
| 2. 제안과 승인 | 미리 실행을 하고 액션을 요청해요 | 선택한 액션을 `with_approval` | 승인하고 사후 확인을 검토해요 |
| 3. 낮은 위험은 자율 실행 | 되돌릴 수 있고 서비스가 끊기지 않는 액션을 스스로 실행해요 | 그 액션만 `autonomous` | 사후 확인과 감사를 검토하고, 기대와 다르면 `none`으로 낮춰요 |

단계를 올리고 내리는 것은 설정이고 자동으로 올라가지 않아요. **지금 만드는 부분**은 액션 목록, 위험 축, 승인 필요 여부, 위임 설정, 감사 로그의 필드예요. **엔드투엔드(E2E) 프로세스 단계에서 얹을 것**은 위임 틀 자체(실적이 쌓이면 올리고 무효가 되면 내리는 규칙, 업무 단위의 적용 단계)예요. 방향은 [방향 메모](process-direction.md)에 있고 아직 결정이 아니에요.

## 14. 기본값(설정)과 측정 계획
이 문서의 숫자는 제품에 내장되는 기본값(설정)이에요. [기본값 표](defaults.md)에 있는 것(보존, 권한 캐시의 경과 시간 등)은 그 표를 따르고, 아래는 그 표에 없는 이 문서의 값이에요. 알파는 객체와 링크 100만 개 수준의 합성 세계로 기능을 테스트하고, 측정해서 조정하는 일은 클로즈드 베타에서 해요.
| 항목 | 기본값 | 클로즈드 베타에서 무엇으로 측정하나 |
|---|---|---|
| 자체 측정의 간격과 제한 시간, 자체 목표(3.5) | 스트림 60초, 배치 15분, 60초, 3.5절 표의 값 | 합성 세계에서 응답 시간과 지연의 분포, 오탐 알림 |
| 역할이 메모리에 보유하는 최근 신호의 길이, 트레이스 샘플링 비율 | 정하지 않음 | 메모리와 트레이스의 비용, 진단에 필요한 구간 |
| 하트비트의 간격과 외부 기준, 설정 사본을 다시 읽는 간격 | 60초, 3배, 30초 | 알림 채널의 지연, 설정이 전파되는 시간 |
| 미리 실행의 유효 시간, 사후 확인을 기다리는 시간 | 600초, 정하지 않음 | 액션별로 효과가 나타나는 시간 |
| 범위가 넓은 액션의 비율, 버전 불일치의 허용 기한, 예측 구간 | 25%, 3600초, 14일 | 알파의 운영 결과, 스토리지의 실제 증가량 |

## 15. 알려진 한계와 미결 사항
**알려진 한계**
- 진단은 고정된 규칙 기반이라 규칙에 없는 새 장애의 원인은 `unknown_cause`예요. 그때 에이전트는 신호를 직접 읽어요. 규칙은 인시던트마다 추가돼요(목록의 버전이 올라가요).
- 자체 측정은 작은 그래프 하나의 경로만 측정해요. 큰 KG의 지연은 부품의 신호로 확인해요. 5.2의 인과 관계처럼 일부는 가정이라, 합성 세계에서 장애를 일으켜(연결 풀 고갈, 워커 정지, 프로젝션 지연) 규칙이 맞는지 검증해요.
- 워커와 엔진이 모두 멈추면 신호의 일부가 비어요. 비어 있음은 알 수 없음이고 정상이 아니에요.
- 스토리지 없이 답하는 동안 권한 캐시가 15분을 넘으면 기본 부여된 읽기만 돼요(5.4).

**미결 사항**
- `sys` 타입의 구체적인 목록과 링크 타입의 이름, 트레이스 샘플링 규칙과 보존 기한, 스토리지 액션의 이름과 축, 메모리에 보유하는 신호의 길이.

## 16. 관련 결정과 요구사항
- 결정: [ADR-0017](adr/0017-self-observability-and-operability.md). 관련 결정은 ADR-0006(역할 4종), ADR-0007(위험 축), ADR-0010(MCP, 사전 정의 질의), ADR-0011(가용성), ADR-0012(역할, 서비스 계정, 감사), ADR-0013(헬스 상태, 알림, 지표 테이블), ADR-0016(REST 규칙).
- 요구사항: REQ-200(역할 4종의 신호), REQ-201(자체 측정), REQ-202(헬스 상태와 `diagnose`), REQ-203(이벤트 코드 목록), REQ-204(운영 액션), REQ-205(설정), REQ-206(권한과 기록), REQ-207(운영 MCP 인터페이스), REQ-208(하트비트, 롤아웃, 증가 예측).
- 같이 읽을 문서: [데이터 관측성](data-observability.md), [KG 운영](kg-operations.md), [접근 제어](access-control.md), [REST API 자원과 경로 (2)](rest-api-resources-graph.md), [KG에 묻기](kg-queries.md).
