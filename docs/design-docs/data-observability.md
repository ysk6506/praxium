---
title: 데이터 관측성 — 신호, 건강, 목표, 알림, 실행 들여다보기
kind: guide
status: active
follows:
  - docs/design-docs/adr/0013-data-observability.md
---
# 데이터 관측성

소스에서 데이터가 잘 들어오는지, 들어온 데이터가 유효한지, 파이프라인이 도는지, 객체가 잘 만들어지는지, 객체·액션·링크가 얼마나 바뀌는지를 보는 설계예요. 신호가 생겨서 저장되고, 건강이 되고, 알림과 답이 되기까지를 다뤄요. 문제 목록의 흐름은 [KG 운영](kg-operations.md)(ADR-0011)이 정해요. 지표와 건강과 알림은 이 문서가 맡아요. 숫자는 모두 재지 않은 가정이에요(16절).

## 1. 무엇에 답하나

| 쟁점 | 답 |
|---|---|
| 소스에서 잘 들어오나, 들어온 데이터가 유효한가 | 연결·받기·유효성의 신호(3절), 기대 조건과 컬럼 통계, 평소와 다름(7절) |
| 파이프라인이 잘 돌고 객체가 잘 만들어지나 | 단계마다의 수 맞춤과 시간, 객체의 신호, 실행 들여다보기(3절, 9절) |
| 객체·액션·링크가 얼마나 바뀌나 | 바뀜의 신호, 출렁임, 시간줄(3절, 9절) |
| 소스 하나가 아프면 무엇이 걸리나 | 정의의 계보를 따른 건강의 번짐과 뿌리 까닭(5절, 10절) |
| 사람과 에이전트가 "믿어도 되나"를 어떻게 묻나 | `health`와 `quality` 질의, 답의 건강 귀띔(11절) |
| 제품 자신과 알림이 죽으면 | 같은 틀로 재고 생존 신호를 둠(13절) |

## 2. 한눈에

**핵심 생각 셋.** 첫째, 관측은 끝에 붙이는 화면이 아니라 실행기가 내는 정식 산출물이에요. 둘째, 건강은 정의의 계보를 따라 흘러요. 셋째, 사람만 묻는 게 아니라 에이전트와 다른 시스템도 API와 MCP로 물어요.

```
 실행기(일꾼, 질의 엔진, API): 단계마다 수와 통계, 늘어난 만큼만 고침
   │ 신호
   ▼
 지표 표 · 실행 기록 · 시간줄 · 쓰임 기록 ── 검사: 기대 조건 · 제품에 든 검사 · 목표 · 평소와 다름
   ▼
 자원마다 건강 ◀── 정의의 계보(소스 → 데이터셋 → 공급 → 타입·속성 → 링크 정의 → 읽는 계정)
   ├─▶ 알림(뿌리 까닭으로 묶음) ─▶ 문제 목록 ─▶ 담당자(보내는 길 포트)
   ├─▶ `health`·`quality` 질의 ─▶ 모든 답의 봉투에 건강의 귀띔(API, MCP)
   └─▶ 열린 형식의 지표·로그·추적, 변경 알림의 길
```

건강 계산, 뭉치기, 알림 평가는 일정에 따라 일꾼이 돌아요. 같은 일정이 두 번 걸리지 않게 키로 막아요(ADR-0011의 6번).

## 3. 신호: 여덟 자리

자원은 소스, 데이터셋, 공급, 파이프라인(소스 하나가 거치는 수집에서 적용까지의 줄), 타입(객체 타입), 속성, 링크 타입, 역량 질문, KG, 시스템 계정이에요. `detail_key`는 지표 안의 갈래예요. 지표 이름은 제품이 싣는 닫힌 목록이에요. 흐름의 "구간"은 짧은 시간(기본 1분, 가정)에 받은 변경 묶음이에요.

| 자리 | 지표 | 뜻 | 단위 | 붙는 자원 | 배치와 흐름 |
|---|---|---|---|---|---|
| 연결 | `connection_ok`, `auth_ok`, `read_only_verified` | 닿았나, 인증이 통했나, 소스 쪽 계정이 읽기 전용으로 확인됐나(어댑터가 확인 못 하면 값이 없고 `unknown`) | 0/1 | 소스 | 배치는 수집 때와 주기 점검 때, 흐름은 구독이 붙은 내내 |
| 연결 | `response_time_ms`, `request_error_ratio`, `rate_limited_count` | 요청 응답 시간(분포), 요청 오류의 비율, 속도 제한에 걸린 수 | ms, 비율, 번 | 소스 | 같음 |
| 연결 | `circuit_open` | 잇달아 실패해 끊었나(인증 실패는 바로 끊음) | 0/1 | 소스 | 배치는 일정이 멈춤, 흐름은 구독을 끊음 |
| 연결 | `collector_alive`, `collector_last_seen_age_s` | 떨어진 망의 수집기가 살았나, 마지막으로 본 뒤 | 0/1, 초 | 소스 | 수집기가 있는 소스만 |
| 받기 | `runs_on_time`, `runs_late`, `runs_skipped` | 일정 칸을 제때 돌았나, 늦었나, 건너뛰었나 | 번 | 소스, 데이터셋 | 배치만. 흐름은 `stream_connected`(0/1, 구독이 붙어 있나) |
| 받기 | `run_duration_s`, `records_received`, `bytes_received` | 한 번 도는 데 걸린 시간(분포), 받은 기록 수와 크기. 평소와의 차이는 7절 | 초, 건, 바이트 | 소스, 데이터셋 | 배치는 실행마다, 흐름은 구간마다 |
| 받기 | `capture_complete` | 온전한 수집인가(쪽 넘김 끝, 알려 준 수와 맞음) | 0/1 | 소스, 데이터셋 | 배치만. 흐름은 맞추기가 대신 |
| 받기 | `schema_changes`, `unreadable_records` | 모양의 바뀜(컬럼 더함·뺌, 자료형 바뀜. `detail_key`가 종류), 읽지 못한 기록 | 건 | 데이터셋 | 같음. 흐름은 첫 기록에서 알아챔 |
| 받기 | `freshness_age`, `reconcile_age_s` | 신선도(ADR-0011, 주기에 대한 나이), 마지막 맞추기 뒤 시간 | 비율, 초 | 소스, 데이터셋 | 흐름의 신선도는 밀림 기준. `reconcile_age_s`는 흐름만 |
| 받기 | `stream_lag_s`, `stream_throughput_eps` | 밀림(마지막으로 적용한 사건의 때와 지금의 차), 처리량 | 초, 건/초 | 소스, 데이터셋 | 흐름만 |
| 받기 | `late_events`, `out_of_order_events`, `duplicate_events` | 늦게 온, 순서가 바뀐, 겹친 사건 | 건 | 데이터셋 | 흐름만. 배치의 겹친 행은 정제가 걸러 수 맞춤에 나옴 |
| 유효성 | `expectation_checked`, `expectation_violated`, `type_mismatch_values` | 기대 조건을 건 수와 어긴 수(`detail_key`가 조건 이름), 다듬어도 값 타입에 안 맞는 값 | 건 | 데이터셋, 공급, 속성 | 배치는 묶음에, 흐름은 기록마다 |
| 유효성 | `quarantined_records` | 어겨서 따로 떼어 둔 기록 | 건 | 공급 | 흐름만. 배치는 `fail`이면 적용을 멈춤 |
| 유효성 | `column_null_ratio`, `column_distinct_est`, `column_top_share`, `column_min`, `column_max` | 컬럼의 빈 값 비율, 서로 다른 값의 어림 수, 가장 흔한 값이 차지한 비율, 수의 범위 | 비율, 개, 컬럼의 단위 | 데이터셋(`detail_key`가 컬럼) | 배치는 실행마다 전부, 흐름은 늘어난 만큼 고침. 민감 컬럼은 범위를 저장 안 함(4.4절) |
| 파이프라인 | `stage_in`, `stage_out`, `stage_dropped`, `stage_balanced` | 단계마다 들어온·나간·떨어진 수(떨어진 수는 이유가 `detail_key`)와 들어온 = 나간 + 떨어진이 맞나 | 건, 0/1 | 파이프라인(`detail_key`가 단계) | 배치는 동기화 한 번, 흐름은 구간 하나 |
| 파이프라인 | `reconciliation_balanced`, `record_outcome` | 기록 하나의 끝 넷(쓰임·걸러 냄·문제·안 쓰임)의 합이 받은 수와 맞나(ADR-0011) | 0/1, 건 | 파이프라인 | 같음 |
| 파이프라인 | `stage_duration_s`, `stage_backlog`, `retry_count`, `error_count` | 단계의 시간(분포), 밀린 양, 다시 한 횟수, 오류(`detail_key`가 오류 코드) | 초, 건, 번 | 파이프라인 | 밀린 양은 배치는 줄에 선 실행 수, 흐름은 못 처리한 사건 수(`detail_key`가 `runs`/`events`) |
| 파이프라인 | `apply_held`, `held_age_s` | 적용을 붙잡았나, 붙잡힌 지 얼마 | 0/1, 초 | 파이프라인 | 배치는 확인을 기다림. 흐름은 `held`이고 받는 것은 계속 |
| 객체 | `objects_total`, `objects_created`, `objects_updated`, `objects_retired` | 타입마다 총수, 만든·고친·물러난 수 | 개 | 타입 | 배치는 적용마다, 흐름은 구간마다 |
| 객체 | `attribute_fill`, `attribute_age` | 채워진 비율(`required`가 분모), 속성 값을 본 때로부터의 나이(분포. 속성마다의 신선도) | 비율, 초 | 속성 | `volatile`은 본 때, `versioned`는 쥔 공급이 온전히 준 때 |
| 객체 | `feed_contribution`, `unattached_values`, `unattached_expired` | 공급이 만들거나 고친 속성 값 수, 붙을 곳을 기다리는 값, 기한이 지나 문제가 된 값 | 개, 건 | 공급 | 붙을 곳 기다림은 살만 붙이는 흐름 공급에서 주로 |
| 객체 | `identity_merges`, `identity_splits`, `identity_candidates` | 합침, 가름, 식별 후보 | 건 | 타입 | 같음 |
| 객체 | `source_conflicts`, `intent_observation_gaps` | 공급 사이의 어긋남, 의도(`configured`)와 관측(`observed`)의 어긋남 | 건 | 타입, 속성 | 같음 |
| 객체 | `orphan_objects`, `completeness_mismatches`, `grade_fact`, `grade_assumed`, `grade_unknown` | 이어지지 않은 객체, 완전성 선언과 어긋남, 근거 등급의 비율(ADR-0011) | 비율, 건 | 타입, 속성(등급은 링크도) | 같음 |
| 링크와 묶음 | `links_total`, `links_created`, `links_removed`, `dangling_links`, `binding_violations` | 링크 타입마다 총수, 만든·없앤 수, 가리키는 열쇠가 없어 못 이은 링크, 바인딩의 개수 어김 | 개, 건 | 링크 타입, 공급 | 같음 |
| 링크와 묶음 | `derived_groups_total`, `derived_groups_changed` | 끌어낸 묶음의 수, 바뀐 수 | 개 | 링크 타입, 타입 | 입력이 배치든 흐름이든 늘어난 만큼 다시 따짐 |
| 링크와 묶음 | `zero_slack_groups`, `exposed_objects` | **이중화의 자세**: 여유가 0인 묶음 수, 하나만 더 죽으면 죽는 객체 수 | 개 | 타입, KG | 배치는 KG 버전을 봉할 때, 흐름은 투영을 고칠 때 |
| 바뀜 | `change_rate`, `version_diff_size` | 변경의 속도(바뀐 객체·링크 수), 봉한 버전 사이의 차이의 크기 | 건/분, 건 | KG, 타입 | 배치는 큰 묶음이 드문드문, 흐름은 작은 묶음이 잦음 |
| 바뀜 | `type_churn`, `attribute_churn` | 구간 안에 바뀐 객체의 비율, 속성 값이 바뀐 횟수(값은 안 남기고 수만) | 비율, 번 | 타입, 속성 | `volatile` 속성도 횟수는 셈 |
| 바뀜 | `flapping_count` | **출렁임**: 창 안에서 값이 되풀이해 뒤집힌(A→B→A) 객체·속성·링크의 수 | 개 | 타입, 속성, 링크 타입 | 흐름에서 두드러지고, 배치는 동기화마다 뒤집히는 것 |
| 바뀜 | `definition_merges`, `definition_reverts` | 정의의 병합과 되돌리기 수 | 건 | KG | 같음 |
| 쓰임 | `query_count`, `query_errors`, `query_latency_ms`, `answer_staleness` | 질의 종류(`detail_key`)마다 수, 오류, 응답 시간(분포), 답에 붙은 신선도(분포) | 건, ms, 초 | KG, 시스템 계정 | 같음 |
| 쓰임 | `rate_limited_calls`, `bulk_reads` | 한도에 걸린 호출, 대량으로 읽기 | 번 | 시스템 계정 | 같음 |
| 쓰임 | `unread_days` | **아무도 안 읽는 것**: 마지막으로 읽힌 뒤 지난 날 | 일 | 타입, 속성, 공급 | 같음 |
| 충족 | `competency_fill_rate`, `competency_not_evaluated` | 역량 질문의 충족률(필요한 것을 다 채운 대상 객체 ÷ 대상 객체, `detail_key`가 필요의 걸음), 한도 때문에 따지지 못한 객체 수. 자세한 것은 ADR-0015 | 비율, 개 | 역량 질문 | 늘어난 만큼 고쳐서 배치든 흐름이든 같음. 속성으로 거른 값은 지표로 쌓지 않고 물을 때 계산 |

- 액션은 지금 타입의 목록뿐이라 정의의 바뀜(`definition_merges`와 타입의 상태 바뀜)으로 재요. 액션을 실행하는 자리는 E2E 프로세스를 구체화할 때 같은 틀에 채워요.
- 이중화의 자세는 `exposure` 질의와 같은 계산이에요(ADR-0010). 질의 엔진이 자기 투영에서 세어 지표 주소로 내고, 일꾼이 거둬 지표 표에 쌓아요(엔진은 저장소에 쓰지 않아요). 가장 많이 바뀌는 객체는 지표가 아니라 `churn_top` 표(4.3절)에 값으로 남아요.

## 4. 지표의 저장

### 4.1 `metric_point`

데이터의 지표는 제품 안에 저장해요. 화면과 API가 지난 흐름을 보여야 하고, KG 버전마다의 값이 남아야 해서예요. ADR-0011의 `quality_metric` 표는 이 표가 이어받아요.

| 칸 | 뜻 |
|---|---|
| `kg_id` | 어느 KG. 제품 부품의 지표는 비어 있어요 |
| `resource_kind` | `source` / `dataset` / `feed` / `pipeline` / `object_type` / `attribute` / `link_type` / `competency_question` / `kg` / `system_account` / `component`(제품 부품, 13절) |
| `resource_id` | 정의 항목의 바뀌지 않는 `id`(이름이 아니에요. 이름을 바꿔도 이어져요) |
| `detail_key` | 갈래. 닫힌 목록이나 정의 항목의 `id`예요(컬럼, 단계, 질의 이름, 기대 조건, 오류 코드, 이유). 행의 값은 안 돼요 |
| `metric`, `window`, `window_start` | 3절의 이름, 구간의 길이(`run`, `1m`, `1h`, `1d`)와 시작 |
| `value`, `numerator`, `denominator` | 값. 비율은 분자와 분모도 남겨요(분모가 바뀐 것을 알 수 있게) |
| `buckets` | 분포 지표의 칸별 수(정해진 경계) |
| `definition_set` | 이 값을 낸 정의 묶음(`D-142` 같은 번호) |
| `kg_version`, `change_seq` | 봉한 버전의 ID와 이 구간이 닿은 끝 `change_seq`. 없을 수 있어요 |

키는 (`kg_id`, `resource_kind`, `resource_id`, `detail_key`, `metric`, `window`, `window_start`)예요. 지표마다의 종류와 단위와 붙는 자원은 지표 목록(`metric_def`)에 제품이 실어요.

진실은 PostgreSQL에 둬요. 지표, 쓰임 기록, 컬럼 통계가 PostgreSQL로 감당되지 않으면 분석용 저장소(열 단위)를 포트 뒤에 둬요. 그것은 끌어낸 것이고, 오래된 값은 오래된 것의 자리로 내려 둘 수 있어요(ADR-0009). 어느 제품인지는 구현하는 변경에서 재서 정해요.

### 4.2 뭉쳐 줄이기

구간을 합칠 때 종류마다 달라요. `counter`(예: `records_received`)는 더해요. `gauge`(`connection_ok`)는 마지막 값, 최소, 최대를 남겨요. `ratio`(`attribute_fill`)는 분자끼리 분모끼리 더해요. `distribution`(`response_time_ms`)은 칸별 수를 더하고, 분위수는 칸에서 어림으로 읽어요(결정적이에요). 구간은 `run`과 `1m`을 짧게, `1h`를 중간, `1d`를 길게 둬요(기한은 가정, 16절). 굵은 구간으로 넘기면 가는 구간을 지워요. 정의 묶음이 바뀐 구간은 합치지 않고 `definition_set`마다 따로 둬서 병합 전후를 가를 수 있게 해요.

### 4.3 가짓수를 묶어 두는 규칙

- **객체 하나하나를 이름표로 붙이지 않아요.** 이름표(칸)가 될 수 있는 것은 4.1의 자원 종류와 정의 항목의 `id`, 닫힌 `detail_key`뿐이에요.
- 행 수는 (자원 수 × 지표 수 × 갈래 수 × 구간 수)로 묶여요. 자원은 정의 항목이라 객체 수에 안 늘어나요. 예: 지어낸 데이터셋 천 개에 컬럼 스물 개면 컬럼 통계만 구간마다 수만 줄이에요. 그래서 컬럼 통계는 `1m` 없이 `run`이나 `1h`부터 남겨요(가정, 재서 정해요).
- 값으로 남기는 상위 목록은 `churn_top`(자원, 구간, 순위 20까지, 객체 참조, 횟수)이에요. 크기가 고정이라 이름표가 아니에요.
- 쓰임 기록은 지표 표가 아니라 하루 단위 집계 표 `usage_record`(시스템 계정, 타입, 속성, 날, 횟수)예요. 계정 수 × 읽은 속성 수로 묶여요. 10절에서 "누가 읽나"에 답해요.

### 4.4 통계를 늘어난 만큼만 고치는 방법

실행기는 변경(더함, 고침, 사라짐)을 처리하면서 컬럼 통계 상태(`column_stat_state`: 데이터셋, 컬럼, 종류, 상태)를 고쳐요. 전체를 다시 읽지 않아요.

- 수와 빈 값의 수는 정확히 세요(더함 +1, 사라짐 -1).
- 서로 다른 값의 수는 정해진 해시와 정해진 크기로 세는 어림이고, 가장 흔한 값은 정해진 크기의 빈도 어림이에요(비율만 지표로 남겨요). 범위는 최소와 최대예요. 이 셋은 빼지 못해서 구간 안에 들어온 기록으로 세고, 전체는 온전한 수집이나 맞추기 때 처음부터 다시 세요.
- 어림도 같은 입력과 같은 순서면 같은 결과예요(해시와 크기가 고정이고, 처리 순서는 `change_seq`로 고정돼요). 학습이나 무작위 표본을 쓰지 않아요.
- **내용이 지표로 새지 않게 해요.** 흔한 값 자체와 범위는 행의 내용이라서, 민감 등급이 `open`이 아닌 컬럼은 수·빈 값·어림 수만 남기고 범위와 흔한 값은 저장하지 않아요. 흔한 값의 실물은 9.2절의 견본 길로만 권한을 따라 보여요.

## 5. 건강

### 5.1 상태 여섯

| 상태 | 뜻 |
|---|---|
| `healthy` | 검사가 다 통과했고 위쪽도 멀쩡해요 |
| `degraded` | 일부가 어긋났어요(`warn`, 목표를 못 지킴). 쓸 수는 있어요 |
| `failing` | 못 쓰는 상태예요(`fail`, 안전장치의 멈춤, 인증 실패, 일정이 저절로 멈춤) |
| `paused` | 사람이 일부러 멈췄어요(운영 역할의 일시 정지). 검사는 남기되 상태는 이것이에요 |
| `unknown` | 잴 신호가 없어요(처음 수집 전, 검사가 못 돌았어요). `healthy`로 읽지 않아요 |
| `affected_upstream` | 자기 검사는 다 통과했는데 위쪽 자원이 `degraded`나 `failing`이에요 |

건강 행(`health_state` 표)의 칸: `resource_kind`, `resource_id`, `health_state`, `own_state`(자기 검사만으로 정한 다섯), `cause_refs`(뿌리 까닭의 목록), `since`, `evaluated_at`, `definition_set`, `kg_version`. 건강을 계산하는 자원은 소스, 데이터셋, 공급, 파이프라인, 타입, 속성, 링크 타입, 역량 질문, KG예요. 역량 질문의 건강은 충족률 목표에서 오고, 못 채우는 까닭이 소스나 공급이면 계보를 따라 뿌리 까닭이 거기로 올라가요. 시스템 계정은 건강이 아니라 영향을 받는 쪽으로만 나와요.

### 5.2 검사의 종류

- **기대 조건**(ADR-0008): 정의로 선언한 닫힌 종류예요(비어 있지 않음, 겹치지 않음, 값의 목록, 범위, 값 타입, 가리킴, 행 수, 모양). `fail`이면 `failing`, `warn`이면 `degraded`이고 문제 목록에도 올라요.
- **목표**(6절): 못 지킨 비율이 허용을 넘으면 `degraded`, 빠르게 나빠지면 `failing`.
- **제품에 든 검사**: 정의 없이 항상 돌아요. 기본값은 가정이에요.

| 제품에 든 검사 | 자원 | 걸리는 때 | 상태 |
|---|---|---|---|
| `connection_failed`, `auth_failed` | 소스 | 닿지 못함, 인증 실패(바로 끊음) | `failing` |
| `schedule_missed` | 소스, 데이터셋 | 일정 칸을 건너뜀 / 일정이 저절로 멈춤(잇달아 5번 실패, ADR-0011) | `degraded` / `failing` |
| `no_data` | 데이터셋 | 온전한 수집인데 받은 수가 0, 또는 흐름이 기대 간격 동안 사건이 없음 | `degraded` |
| `schema_changed`, `guard_blocked` | 데이터셋, 파이프라인 | 받은 모양이 바뀜, 안전장치가 적용을 멈춤(흐름은 `held`) | `failing` |
| `stale`, `lagging` | 소스, 속성 | 주기의 2배를 넘음(ADR-0011), 흐름의 밀림이 기대를 넘음 | `degraded` |
| `unbalanced` | 파이프라인 | 단계의 수 맞춤이 안 맞음 | `degraded` |

### 5.3 자원 하나의 건강을 정하는 규칙

위에서부터 처음 맞는 것이에요.

1. 운영 역할이 일부러 멈춘 소스와 그 줄은 `paused`.
2. 자기 검사 중 `failing`을 내는 것이 있으면 `failing`.
3. `degraded`를 내는 것이 있으면 `degraded`.
4. 돌아야 할 검사가 하나도 못 돌았거나 신호가 없으면 `unknown`.
5. 자기 검사는 다 통과했는데 계보의 위쪽에 `failing`이나 `degraded`가 있으면 `affected_upstream`.
6. 그 밖에는 `healthy`.

묶음 자원은 아래에서도 받아요. **타입**은 객체를 만드는 공급(`creates_objects`)의 건강을, **속성**은 그것을 쥔 공급의 건강을 받아요. 속성을 공급 둘이 주는데 이기는 순서 첫째가 아파도 다음 공급이 값을 대신 주니 `degraded`예요(둘 다 아프면 `failing`). **KG**는 자기 검사와 소스 수를 세어(`failing` 몇, `degraded` 몇) 보여요. 한 소스가 아프다고 KG 전체가 `failing`이 되지는 않아요.

### 5.4 계보를 따른 번짐과 뿌리 까닭

번지는 길은 정의의 계보예요(ADR-0008): 소스 → 데이터셋 → 정제 → 공급 → 타입·속성 → 링크 정의 → 끌어낸 묶음 → 읽는 시스템 계정. 컬럼과 속성 단위까지 따라가므로, 컬럼 하나의 모양이 바뀌면 그 컬럼이 닿는 속성만 아파요.

- **아래로 번져요.** 자기 검사가 어긋난 자원에서 출발해 후손에게 `affected_upstream`과 `cause_refs`를 붙여요.
- **거꾸로 올라가요(뿌리 까닭).** 어긋난 자원이 기대는 위쪽에도 어긋난 자원이 있으면, 더 위가 없는 어긋난 자원까지 올라가 그것을 뿌리로 적어요. 뿌리가 여럿이면 다 적어요.
- **까닭이 위에 있는 어긋남은 자기 알림을 만들지 않고 뿌리에 묶여요.** 신선도·채움·받는 양 목표를 어긴 속성의 공급이 아픈 소스에 기댄 경우가 그래요.

지어낸 예시예요. 스위치 재고 소스 `switch_inventory`의 인증이 만료돼요.

```
소스 switch_inventory       auth_failed → failing   ◀── 뿌리 까닭(마지막 온전한 수집 27시간 전)
 ├ 데이터셋 switch_devices ─ 공급 feed_devices ─────▶ 타입 NetworkDevice        affected_upstream
 └ 데이터셋 switch_ports ─┬─ 공급 feed_ports ───────▶ 타입 Interface            affected_upstream
                          └─ 공급 feed_lag_links ───▶ 끌어낸 Lag 묶음            affected_upstream
                                      └──────────────▶ 읽는 계정 acct_capacity_report, acct_netops_agent
```

소스는 인증 실패로 바로 `failing`이 되고 일정이 멈춰요. 데이터셋 둘, 공급 셋, `NetworkDevice`·`Interface`, `Lag` 묶음이 `affected_upstream`이에요. 몇 주기 뒤 `Interface`의 `oper_status`(`dynamic`)가 신선도 목표를 못 지켜 자기 검사로도 `degraded`가 되지만, 까닭이 위쪽이라 알림은 소스의 알림 하나에 묶여요. 계정 둘은 10절의 "누가 읽나"로 나오고, 알림의 받는 쪽은 소스의 담당자예요. 거꾸로 `NetworkDevice`의 값이 묵은 까닭을 물으면(11절) 소스의 인증 실패 하나가 나와요.

## 6. 목표

목표는 정의의 항목이에요(버전이 있고 제안과 승인을 거쳐요). 칸: `objective_id`(바뀌지 않는 `id`), `target`(붙는 자원: 공급, 타입, 속성, 역량 질문), `kind`(`freshness` 얼마 안에 / `completeness` 채워진 비율 이상 / `validity` 어김 비율 이하 / `volume` 받는 양이 최소~최대 안 / `competency_fill` 충족률 이상), `threshold`(종류에 맞는 값), `period`(못 지킨 비율을 세는 기간, 기본 28일), `allowed_miss`(기간 중 못 지켜도 되는 비율), `severity`.

**못 지킨 비율의 계산.** 판정 간격마다(배치는 실행마다, 흐름과 신선도는 기본 5분, 가정) 목표를 "지킴 / 못 지킴 / 모름"으로 판정해요. 못 지킨 비율 = 못 지킨 수 ÷ (지킨 수 + 못 지킨 수). 모름은 분모에서 빼고 `unknown_share`로 따로 보여요(모름이 많으면 목표는 `unknown`). 남은 허용 = `allowed_miss` − 못 지킨 비율.

**빠르게 나빠짐의 판정.** 소진율 = (짧은 창의 못 지킨 비율) ÷ `allowed_miss`예요. 짧은 창(기본 1시간)과 긴 창(기본 6시간)의 소진율이 둘 다 기준(기본 6, 가정)을 넘을 때만 "빠르게 나빠짐"이에요. 잠깐의 튐과 느린 악화를 둘 다 거르려는 거예요. 이때 건강은 `failing`이고, 못 지킨 비율만 넘었으면 `degraded`예요.

**속성의 바뀌는 빠르기가 주는 기본값.** 표준 모델이 속성마다 적은 `change_class`(ADR-0007)가 기본 목표를 정해요. 목표를 안 적어도 아래가 서요. 값은 가정이에요.

| `change_class` | 신선도 | 온전함 | 뒤바뀜 |
|---|---|---|---|
| `static` | 목표 없음(안 바뀌는 것은 묵어도 괜찮아요) | `required`면 99% 이상 | 바뀐 비율이 아주 낮아야 하고, 넘으면 "식별이 흔들려요" |
| `slow` | 공급 주기의 2배 이내 | `required`면 95% 이상 | 목표 없음 |
| `dynamic` | 공급 주기의 2배(흐름은 밀림 1분 이내) | 목표 없음 | 출렁임(`flapping_count`)을 봐요 |

## 7. 평소와 다름

방법은 정해진 통계 하나예요. 학습 모델을 안 써서 같은 입력이면 같은 판정이 나오고 까닭을 말할 수 있어요.

- **대상**: `records_received`, `objects_created`·`objects_retired`, `links_created`·`links_removed`, `change_rate`, `run_duration_s`, `column_null_ratio`, `column_distinct_est`, `column_top_share`.
- **평소**: 같은 요일, 같은 시간대의 지난 값 여럿(기본 4주, 가정)의 가운데 값(중앙값)과 흩어짐(중앙값에서의 절대 편차의 가운데 값).
- **점수** = |지금 − 가운데 값| ÷ max(흩어짐 × 1.4826, 바닥). 바닥은 0으로 나누지 않게 하는 최소 폭(가운데 값의 5%와 1건 중 큰 것, 가정)이에요.
- **판정**: 점수가 문턱(경고 4, 심각 6, 가정) 이상이고 차이가 최소 크기 이상일 때만 "평소와 다름"이에요(작은 수의 오탐을 막으려는 거예요). 지금 값, 가운데 값, 흩어짐, 점수, 바탕의 개수를 함께 남겨서 "왜"를 말해요.
- **처음이라 지난 값이 없을 때**: 바탕이 최소 개수(기본 3, 가정)보다 적으면 판정은 `insufficient_history`이고 건강에 영향이 없어요. 화면과 답에는 "바탕을 쌓는 중"으로 보여요.
- **정의나 일정이 바뀐 직후**의 구간은 바탕을 끊지 않고 판정에 `definition_changed`를 붙여요. 알림 글에 "`D-142` 병합 뒤"가 함께 나와요.
- **문턱의 기본값은 가정이에요.** 규칙마다 고칠 수 있고, 알파에서 거짓 알림과 놓친 것을 세어 고쳐요.

## 8. 알림

### 8.1 규칙

규칙 종류는 닫힌 아홉이에요: `threshold`(문턱), `anomaly`(평소와 다름), `no_data`, `schedule_missed`, `schema_changed`, `expectation_violated`, `lagging`, `objective_burning`(목표가 빠르게 나빠짐), `health_changed`. 칸: `rule_id`, `kind`, `target`(자원이나 조건), `condition`(종류에 맞는 칸), `severity`(`info` / `warning` / `critical`), `recipients`(비워 두면 자원의 담당자(ADR-0012). 담당자가 없는 자원과 제품 자신은 `admin`), `for`(이 시간 이어져야 알려요), `group_by`(기본 `root_cause`), `working_hours`(받는 쪽의 작업 시간대. 밖이면 `critical`만 바로 보내고 나머지는 시간대가 열릴 때 한 번에 보내요), `escalation`(확인 기한과 다음 사람). **규칙도 정의의 항목이에요**(버전, 브랜치, 제안을 타요. ADR-0014). 제품이 기본 규칙을 싣고, 고치는 것은 그 자원(소스나 객체 타입)을 범위에 가진 `editor`가 제안하고 `approver`가 승인해요. 알림을 확인하고 잠시 끄고 넘기는 것은 정의가 아니라 운영의 일이라 `operator`가 해요. `paused` 자원에는 `no_data`와 `schedule_missed`를 걸지 않아요.

### 8.2 뿌리 까닭으로 묶기

알림 하나(`alert`)는 `group_key` = (뿌리 자원, 뿌리 검사)마다 하나예요. 소스 하나가 죽어도 타입 백 개의 알림이 가지 않아요. 알림에는 영향받는 자원의 **수와 종류별 개수**가 붙고, 자세한 목록은 열면 10절의 계산으로 나와요. 뿌리가 바뀌거나 심각도가 오르면 같은 알림이 고쳐져 다시 알려요.

### 8.3 상태의 흐름

```
open ──확인함──▶ acknowledged ──잠시 끔(시간)──▶ snoozed ──시간이 지남, 조건이 이어짐──▶ open
 └─기한 안에 확인 없음─▶ escalated(다음 사람, 다시 기한)
(어느 상태에서든) 조건이 풀림(연속 3번, 가정) ─▶ resolved ─같은 뿌리가 다시 어긋남─▶ open(같은 `issue_id`, 재발)
```

확인함, 잠시 끔, 넘기기는 자원의 범위를 가진 `operator`가 해요. 안 받은 알림은 `escalation`의 다음 사람에게 가고, 끝까지 없으면 `admin`이에요.

### 8.4 알림은 문제 목록의 항목이에요

받은 편지함은 하나예요. 알림은 문제 종류 `data_alert`의 항목(ADR-0011의 `issue`)이 되고 `cause_key`는 `group_key`예요. 문제의 상태 셋과는 이렇게 맞아요: `open` ↔ `open`, `acknowledged`·`snoozed`·`escalated` ↔ `in_progress`, `resolved` ↔ `resolved`. 푸는 길은 문제 목록과 같아서, 원인이 규칙(기대 조건, 목표, 매핑)에 있으면 브랜치를 열어 고쳐요. 알림이 KG를 직접 고치지 않아요.

### 8.5 보내는 길

보내는 길은 포트 뒤의 어댑터예요(메신저, 메일, 변경 알림의 길). 포트의 입력은 알림 봉투(`alert_id`, 심각도, 제목, 뿌리 까닭, 영향의 수, 받는 사람, 열어 볼 길)이고 출력은 전달됨 / 실패 / 다시 시도예요. 글은 틀에서 만들어서 행의 내용과 비밀 값이 없어요. 전달에 실패하면 다음 길로 돌리고 `notify_delivery_failures`로 세요(13절).

## 9. 실행 들여다보기와 시간줄

### 9.1 실행 기록

동기화 한 번(`sync_run`, ADR-0011)과 흐름의 구간 하나가 기록 하나예요. 단계마다 한 줄(`run_stage`)을 더해요.

| 칸 | 뜻 |
|---|---|
| `run_id`, `stage`, `attempt` | 실행, 단계, 점유 번호 |
| `started_at`, `ended_at`, `status` | 시간과 결과 |
| `in_count`, `out_count`, `dropped_count` | 들어온·나간·떨어진 수. 들어온 = 나간 + 떨어진이 맞아야 해요 |
| `dropped_by_reason` | 이유별 떨어진 수(단계마다 닫힌 목록: `filtered_by_rule`, `invalid_value`, `quarantined`, `duplicate`, `unreadable`, `excluded_by_exception` 등) |
| `capture_range` | 쓴 원본 기록의 구간(흐름은 `change_seq`의 처음~끝) |
| `definition_set`, `executor_version` | 쓴 정의 묶음, 실행기의 의미 버전 |
| `diff_summary`, `guard_verdict`, `trace_id` | 타입마다 더함·고침·물러남의 수, 안전장치의 판정과 이유, 추적 번호 |

한 동기화의 수 맞춤은 원본 기록 하나가 끝에 넷(쓰임, 규칙이 걸러 냄, 문제, 안 쓰임) 중 하나가 되어 합이 받은 수와 같아야 해요(ADR-0008).

### 9.2 떨어진·떼어 둔 기록의 견본과 권한

견본은 `rejected_sample`(`run_id`, 단계, 이유, 원본 기록 참조)로 이유마다 몇 건(기본 20, 가정)만 남겨요. 내용은 복사하지 않고 읽을 때 원본 기록에서 가져와요. 그러면 보관 기한이 지난 내용은 "지워짐"으로 보여요. 견본은 행의 내용이라서 보통의 읽기와 달라요. 그 소스를 범위에 가진 `operator`·`editor`·`admin`만 읽고, 컬럼마다 민감 등급 한도를 따라 "가려짐"이 보여요. 견본은 사람이 쓰는 HTTP 길에서만 나오고 MCP와 열린 형식의 내보내기에는 안 나가요.

### 9.3 추적 번호와 다시 돌리기

- 동기화 한 번(또는 흐름의 구간 하나)이 추적 하나예요. 단계와 소스 요청이 각각 구간(span)이고, 같은 `trace_id`가 로그와 실행 기록에 찍혀요. 이름표와 수만 있어요.
- **다시 돌리기**는 받아 둔 원본 기록의 그 구간을 같은 정의 묶음과 같은 실행기 버전으로 뒤 단계(정제에서 차이까지)만 다시 돌려 단계별 수와 떨어진 기록을 보는 후보 실행이에요. 소스에 다시 가지 않고 KG를 바꾸지 않아요. 결정성 덕에 원래 결과와 같은지 견줄 수 있고, 브랜치의 정의로 돌리면 "고치면 이 기록들이 살아나나"를 봐요. 운영 역할이 해요.

### 9.4 자원의 시간줄

`timeline_event`의 칸: `at`, `kg_version`/`change_seq`, `resource_kind`, `resource_id`, `event_kind`, `severity`, `ref`(실행, 알림, 제안, `D-N`), `summary`(틀로 만든 짧은 글), `definition_set`. 사건의 종류는 닫힌 목록이에요.

- 실행: `sync_succeeded`, `sync_failed`, `sync_waiting_confirmation`, `stream_held`, `stream_released`, `schedule_paused`, `schedule_resumed`, `schema_changed`, `guard_blocked`, `guard_confirmed`
- 건강과 알림: `health_changed`, `alert_opened`, `alert_acknowledged`, `alert_snoozed`, `alert_escalated`, `alert_resolved`
- 정의와 KG: `proposal_opened`, `definition_merged`, `definition_reverted`, `emergency_revert`, `type_status_changed`, `kg_version_sealed`, `read_pinned`, `read_unpinned`

자원의 시간줄은 그 자원의 사건에 계보 위아래 자원의 사건을 고를 수 있게 합쳐요. 지표 선을 겹쳐 놓으면 "`D-142`가 들어간 뒤 `Interface`의 객체 수가 줄었다"를 눈으로 봐요.

## 10. 데이터 사고의 영향

공급이나 소스가 아프면 무엇이 걸리는지를 둘로 답해요. (1) **정의의 계보**에서 후손을 모아요: 데이터셋, 공급, 컬럼 단위로 닿는 타입과 속성, 링크 정의, 끌어낸 묶음, 저장해 둔 범위. (2) **쓰임 기록**(`usage_record`)에서 그 타입·속성을 최근 N일(기본 30일, 가정) 안에 읽은 시스템 계정을 쓰임이 많은 순으로 모아요. 같은 계산이 고치기 전의 "무엇이 걸리나"(ADR-0008의 정의의 계보)와 한 길이에요. 화면에는 "이 소스가 아파서 타입 2개, 속성 11개, 끌어낸 묶음 1개, 계정 2개가 걸려요"로 보이고, 열면 목록이에요.

**영향 범위 질의의 답에 붙는 표시.** `impact`, `exposure`, `paths`의 답이 쓴 객체 타입, 링크 타입, 속성(채널의 입력)의 공급 중에 `failing`·`degraded`·`stale`이 있으면 답에 `data_health_notes`가 붙어요. 예: "이 답이 쓴 링크 타입 `MEMBER_OF`의 공급 `feed_lag_links`가 소스 `switch_inventory`의 인증 실패로 27시간 묵었어요". 표시는 타입·링크 타입 단위예요. 어느 객체가 묵었는지까지는 몰라요(값마다 출처를 저장하지 않아서예요, 17절). 관측을 쓴 답(`as_observed`)에는 관측의 나이도 함께 붙어요.

## 11. 묻는 길

`health`와 `quality`는 닫힌 질의(ADR-0010)이고 HTTP API와 읽기 전용 MCP에 똑같이 나와요.

| 질의 | 입력 | 출력 |
|---|---|---|
| `health` | `resource`(`{kind, id}`) 또는 `scope`(자원 종류, 상태 목록, 담당자, 소스), `include`(`checks`, `cause`, `downstream`, `impact`, `timeline`), `as_of`(지금이 기본, 또는 봉한 KG 버전), `limit`, `cursor` | `health_state`, `own_state`, `since`, `checks`(`{check, kind, state, observed, threshold, since}`), `cause`(뿌리 자원과 그 검사, 거기까지의 길), `affected`(종류별 개수와 목록, 잘렸는지), `readers`(읽는 계정과 마지막으로 읽은 때), `timeline` |
| `quality` | `resource`나 `scope`, `metrics`(이름 목록), 기간(`from`, `to`) 또는 `kg_versions`, `window`, `compare`(`previous_version` / `same_weekday`), `include`(`objectives`, `anomalies`) | 지표 선 `{resource, metric, detail_key, points:[{at, value, numerator, denominator, kg_version}]}`, 목표의 `{miss_ratio, budget_left, state}`, 평소와 다름의 `{score, median, spread}` |

**답의 봉투에 붙는 건강의 귀띔.** 모든 답의 봉투(KG 버전, 신선도, 근거 등급, 잘렸는지, ADR-0010)에 `health_hint`가 붙어요: `has_unhealthy_inputs`, `worst_state`, `total`, 그리고 자세히를 고르면 `items`(자원, 상태, 뿌리 까닭)를 몇 개까지. 간단히를 고르면 앞의 셋만 가요. 귀띔은 읽을 수 있는 범위만 보여요. 읽을 수 없는 타입의 것은 "가려진 자원 N개"로 수만 나와요(ADR-0012).

**MCP에서의 모양.** 에이전트가 "이 스위치가 죽으면 어디까지 번지나, 믿어도 되나"를 물어요.

```
impact(objects=[sw-0417], view=as_observed)
  → 답 … , health_hint: {has_unhealthy_inputs: true, worst_state: "affected_upstream",
      total: 3, items: [{resource: "Interface", state: "affected_upstream", cause: "switch_inventory"}]}
health(resource={kind: "object_type", id: "Interface"}, include=["cause", "readers"])
  → health_state: "affected_upstream", cause: [{resource: "switch_inventory", check: "auth_failed", since: "27h"}]
```

에이전트는 답을 "묵은 구성 기준"이라고 표시해 넘기고 소스의 담당자에게 알리는 길을 골라요. MCP의 답은 크기에 상한이 있고 오류가 다음에 할 일을 말해요(ADR-0010). 두 질의 모두 시스템 계정별 호출 한도를 받아요.

## 12. 내보내기

- **열린 형식으로 지표·로그·추적을 내요.** 조직이 이미 가진 관측 도구가 가져가요. 열린 관측 형식(OpenTelemetry가 정한 지표·로그·추적의 세 신호와 전송 규약)을 쓰고 특정 도구에 묶이지 않아요. 지표는 제품 안과 같은 이름과 이름표(자원 종류, `resource_id`, `detail_key`)를 써요. 컬럼과 속성 단위의 지표는 가짓수 때문에 기본으로 꺼 두고 켠 것만 내요. 추적은 동기화 한 번이 하나이고 단계가 구간이에요.
- **데이터 건강의 사건은 변경 알림의 길(ADR-0010)로도 나가요.** 같은 길에서 사건의 `kind`로 KG의 변경과 가려요: `health_changed`, `alert_opened`, `alert_resolved`, `guard_blocked`, `stream_held`. 먼저 가져가기 방식이 기본이고 밀어 주기는 뒤에 와요.
- **내용과 비밀 값이 없어요.** 내보내는 것은 지표 목록과 사건 목록에 든 칸뿐이고 자유로운 글 칸이 없어요. 로그에는 이름표와 수만 찍고(ADR-0008) 견본은 9.2절의 길로만 나가요. 이 성질은 시험이 건다(내보내는 모든 칸이 목록에 있는지).

## 13. 제품 자신의 건강과 생존 신호

제품 부품(`component`)도 같은 틀(지표 표, 건강, 알림)로 재요. 받는 쪽은 `admin`이에요. 지표: `worker_alive`와 `lease_takeovers`(일꾼이 살았나, 죽은 일을 이어받은 수), `queue_depth`와 `oldest_job_age_s`(줄에 선 일 수, 가장 오래된 일의 나이), `db_unavailable_count`와 `db_replication_lag_s`(저장소에 못 닿은 수, 복제의 밀림), `engine_memory_bytes`와 `engine_loaded_version_age_s`와 `projection_lag_seq`(질의 엔진의 메모리, 올려 둔 버전의 나이, 반영하지 못한 `change_seq`의 수), `api_error_ratio`, `notify_delivery_failures`(알림이 전달 못 된 수).

**알림이 죽었는지 아는 길.** 알림이 안 오면 아무 문제가 없는 건지 알림이 죽은 건지 못 가려요. 그래서 (1) 보내는 길마다 주기적으로 생존 신호(`heartbeat`)를 어댑터 끝까지 보내고, (2) 마지막 생존 신호의 나이(`alert_heartbeat_age_s`)와 건강 계산이 마지막으로 돈 나이(`health_evaluator_age_s`)를 열린 형식으로 내요. 조직의 바깥 관측 도구가 이 나이가 커지는 것을 보고 제품과 따로 알려요. 저장소가 멈춰도 질의 엔진은 답하므로(ADR-0011) 엔진의 지표는 엔진 자신이 열린 형식으로 계속 내요.

## 14. 정의의 바뀜과 타입의 관리를 보는 것

- `proposals_open`, `proposal_oldest_open_age_s`: 열린 제안의 수와 가장 오래된 것의 나이(KG).
- `merge_check_failures`: 병합 검사의 실패 수. `detail_key`가 검사(유효성, 정의의 시험, 후보 실행, 안전장치)예요(KG).
- `reverts`, `emergency_reverts`, `post_review_overdue`: 되돌리기, 급할 때 되돌리기, 사후 검토 기한을 넘긴 수. `breaking_changes`: 깨는 변경의 수(KG).
- `retired_type_in_use`: 물러났는데 매핑이나 객체가 아직 쓰는 타입(타입). `experimental_type_age_days`: `experimental`로 오래 머문 타입의 나이(타입).
- `similar_type_pairs`: 이름·속성·식별 키가 겹쳐 비슷해 보이는 타입의 짝의 수. 비슷한 타입 찾기(ADR-0007)와 같은 계산이에요(KG).
- `read_pin_age_s`: 묻는 쪽을 옛 버전에 고정해 둔 시간(KG).

`unread_days`(3절)와 함께 모델 관리 역할(`model_steward`)에게 주기로 보여요. 병합 직전·직후의 지표를 `definition_set`으로 견줘 "정의 변경의 효과"를 봐요.

## 15. 알파의 화면 범위

백엔드는 다 만들고 화면은 기능을 시험할 만큼만 둬요(ADR-0006). 자원의 건강 목록(상태, 자원 종류, 담당자로 거름) / 자원 하나의 건강(검사, 뿌리 까닭, 아래로 번진 것, 시간줄 목록) / 실행 목록과 상세(단계별 수, 견본) / 타입의 공급 표(ADR-0008의 표에 건강 칸이 들어 있어요) / 문제 목록(알림 포함, 확인함·잠시 끔). 알림 규칙과 목표의 편집은 API로만 하고(정의의 변경이라 제안을 타요), 충족률은 역량 질문 화면(ADR-0015)이 보이고, 계보 그림과 지표 차트는 클로즈 베타 때 설계해요.

## 16. 숫자는 가정이에요

| 숫자 | 지금 쥔 값 | 어떻게 재서 정하나 |
|---|---|---|
| 지표 구간과 보관(`run`·`1m`·`1h`·`1d`) | 짧게·중간·길게(기한은 정하지 않음) | 지어낸 3천만 객체 세계에서 행 수와 저장의 늘어남 |
| 흐름의 구간 길이, 판정 간격 | 1분, 5분 | 건강 계산의 부하와 알림의 지연 |
| 평소의 바탕과 최소 개수, 문턱 | 4주, 3개, 경고 4·심각 6·바닥 5% | 지어낸 세계에 요일 무늬를 넣어 거짓 알림 수와 놓친 수 |
| 목표의 기간, 소진율 창과 기준, 기본값(6절 표) | 28일, 1시간·6시간·6, 표의 값 | 알파에서 알림 피로와 늦은 알림의 균형, 소스의 실제 주기와 지연 |
| 출렁임의 기준 | 창 안에서 3번 뒤집힘 | 흐름 공급의 실제 뒤집힘 |
| 알림의 해소 조건, 넘기기 기한 | 연속 3번, 정하지 않음 | 알파의 처리 시간 |
| 견본의 수, 쓰임의 창 | 이유마다 20건, 30일 | 알파의 사용 |
| 건강 계산의 시간 | 정하지 않음 | 지어낸 정의 항목 수만 개로 |

## 17. 알고 가는 한계

- 건강은 **받은 데이터**의 건강이에요. 소스가 틀린 값을 온전히 주면 기대 조건으로 선언한 만큼만 잡아요. 완전성 선언이 없는 곳은 "빠졌다"를 모르고 "모른다"예요(ADR-0011).
- 답에 붙는 건강의 표시는 타입·링크 타입 단위예요. 값마다 출처를 저장하지 않기 때문이에요(ADR-0008).
- 평소는 같은 요일·시간대만 봐요. 월말, 연휴, 분기말의 무늬는 못 따라서 그때는 거짓 알림이 나요(잠시 끔으로 넘겨요).
- 서로 다른 값·흔한 값·범위는 빼기가 안 돼서 구간 기준이에요(4.4절).
- 이중화의 자세는 질의 엔진의 계산에 기대서, 엔진의 지표가 일꾼에 닿는 길이 막히면 그동안 값이 비어요.
- 스트리밍의 처리량과 밀림은 조사에서 재지 못했어요. 지표의 이름과 뜻은 정했고 값의 기대는 재서 정해요. 액션의 실행을 재는 자리는 비어 있어요(E2E 프로세스 때).

## 18. 아직 정하지 않은 것

- 지표와 건강 이력, 해결된 알림의 보관 기한.
- 건강 계산과 뭉치기를 맡는 일꾼의 일정 간격과 부하 상한. 계보의 번짐을 변경마다 늘어난 만큼 다시 따질지, 일정마다 전부 다시 따질지.
- 첫 보내는 길의 어댑터 종류.

## 19. 관련 결정과 요구사항

- 결정: [ADR-0013](adr/0013-data-observability.md). 이어진 것은 ADR-0006(역할 셋, 알파), ADR-0007(근거 등급, 타입의 관리), ADR-0008(수 맞춤, 안전장치, 기대 조건, 정의의 계보), ADR-0009(변경의 이어짐, 신선도), ADR-0010(닫힌 질의, 답의 봉투, 변경 알림), ADR-0011(문제 목록, 신선도, 가용성), ADR-0012(역할, 담당자, 민감 등급), ADR-0014(알림 규칙과 목표를 정의로), ADR-0015(역량 질문과 충족률).
- 요구사항: REQ-180(여덟 자리의 신호), REQ-181(건강과 번짐, 뿌리 까닭), REQ-182(목표), REQ-183(평소와 다름), REQ-184(알림), REQ-185(실행 들여다보기와 시간줄), REQ-186(데이터 사고의 영향), REQ-187(`health`·`quality`와 건강 귀띔), REQ-188(지표의 저장과 내보내기), REQ-189(이중화의 자세, 출렁임, 안 읽히는 것). 넓혀진 것은 REQ-150~152.
- 같이 읽을 문서: [KG 운영](kg-operations.md), [현실 데이터 받기](ingestion.md), [KG에 묻기](kg-queries.md), [KG의 저장과 버전](kg-storage.md), [KG의 모델](kg-model.md), [표준 모델](standard-model.md), [접근 제어](access-control.md).
