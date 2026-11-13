---
title: REST API 자원과 경로 (1) — 모델, 수집, 실행, 정의
kind: reference
status: active
follows:
  - docs/design-docs/adr/0016-frontend-separate-rest-api-contract.md
---
# REST API 자원과 경로 (1): 모델, 수집, 실행, 정의

[REST API 설계](rest-api.md)의 규칙 아래에서 아홉 그룹 가운데 앞의 넷(모델, 수집, 실행, 정의)의 길을 적어요. 뒤의 다섯(KG, 관측, 역량 질문, 권한, 바탕)은 [다음 문서](rest-api-resources-graph.md)에, 에이전트 시대의 정의와 푸시 수집과 매핑을 돕는 경로와 시크릿, 운영, 목록은 [더한 기능의 경로](rest-api-resources-additions.md)에 있어요. 이 문서의 자원은 모두 배포에 하나라서 길이 `/v1/...` 바로 아래예요(KG에 든 것은 `/v1/kgs/{kg}/...`, rest-api.md 3-15).

> **길과 칸은 다음 변경의 OpenAPI 문서에서 굳어요.** 길은 `/v1/...`이고 행위는 `:행위` 표기 하나예요(rest-api.md 3-8).

## 1. 읽는 법
- **칸 이름은 원천 문서의 이름을 그대로 써요.** 새로 지어내지 않았어요. 칸의 뜻은 원천 문서에 있어요.
- **역할 필드.** 읽기는 `읽기`라고 적어요. 모든 역할이 자기 범위의 읽기를 포함해서(권한 설계 4절) 범위 안이면 읽을 수 있어요. 그 밖의 일은 여덟 역할의 이름으로 적어요. 범위가 있는 일은 부여의 범위 안에서만이에요.
- **작업 필드.** `작업`이면 202와 작업 자원(rest-api.md 3-7)이에요. `-`는 바로 답해요.
- **쓰기의 길.** 정의를 고치는 모든 일은 브랜치 안의 항목(4그룹)으로 해요. 모델과 받기의 자원에 `POST`/`PATCH`/`DELETE`는 없어요. 읽기는 `?definition_set=<set_no>`로 옛 묶음을, 기본은 메인 브랜치의 끝을 읽어요. 브랜치의 모습은 `?branch_id=<id>`(그 브랜치의 오버레이를 얹은 모습)로 읽어요.
- **항목의 `kind`**와 자리는 [정의](definitions.md) 3-1이 정해요. 이 문서의 읽기 자원은 그 항목을 사람이 읽는 모양으로 낸 거예요.
- **모든 목록**은 `limit`, `cursor`, `filter[..][..]`, `sort`, `fields`를 받아요. 거르는 칸은 표에 적은 것이 전부예요(고정 목록).

## 2. 모델
객체 타입, 링크 타입, 액션 타입, 값 타입, 특성, 층은 모두 정의의 항목이거나 제품이 싣는 데이터예요(ADR-0007). 타입과 속성의 표시 이름은 언어별(`display_names`)로 응답에 실려요. 값 타입, 링크 의미, 특성, 채널은 제품이 싣는 거라 항목이 아니고 읽기만 돼요.

| 길 | 메서드 | 하는 일 | 역할 | 작업 | 원천 |
|---|---|---|---|---|---|
| `/v1/object-types` | GET | 객체 타입 목록. 필터링: `layer`, `status`, `namespace`(`std`, `org`, `ext`), `trait`, `name`(`prefix`) | 읽기 | - | kg-model |
| `/v1/object-types/{type_id}` | GET | 한 타입: 속성(`name`, `value_type`, 단위), 식별 키, 바인딩, 특성, `status`, `superseded_by`, `display_names`, 담당자 | 읽기 | - | kg-model |
| `/v1/link-types` | GET | 링크 타입 목록. 필터링: `meaning`(열하나), `from_type`, `to_type` | 읽기 | - | kg-model |
| `/v1/link-types/{link_type_id}` | GET | 한 링크 타입: 뜻, 출발과 도착의 타입, 개수, 매개 값, 잇는 조건 | 읽기 | - | kg-model |
| `/v1/link-types/{link_type_id}/bindings` | GET | 그 링크 타입의 바인딩(어느 공급과 링크 정의가 이으나) | 읽기 | - | kg-model |
| `/v1/action-types`, `/v1/action-types/{id}` | GET | 액션 타입 | 읽기 | - | kg-model |
| `/v1/value-types` | GET | 값 타입(바탕, 검사, 다듬기) | 읽기 | - | kg-model |
| `/v1/traits` | GET | 특성과 그 특성이 요구하는 속성 | 읽기 | - | kg-model |
| `/v1/layers` | GET | 층의 고정 목록과 층마다의 타입 수. 프락시움 자신의 부품(`sys` 타입)은 `system` 층에 놓여요 | 읽기 | - | kg-model |
| `/v1/object-types/{type_id}/usage` | GET | 타입의 쓰임: 정의의 `what_depends`(12-4)와 같은 답을 이 타입에서 시작해 줘요 | 읽기(정의를 읽는 범위) | - | definitions |
| `/v1/type-usage` | GET | 타입별 쓰임의 보고(몇 객체, 몇 피드, 마지막으로 쓰인 때). 올리기와 합치기를 가리는 근거 | 그 타입을 범위에 가진 `viewer`(`model_steward`는 전체) | - | kg-model 13.3 |
| `/v1/similar-type-pairs` | GET | 겹치는 타입의 짝(주기로 계산한 목록) | `model_steward` | - | kg-model 13.3 |
| `/v1/type-similarity-checks` | POST | 만들려는 타입의 초안(이름, 속성, 식별 키, 매핑될 데이터셋)을 받아 비슷한 타입을 찾아 줘요. 상태를 바꾸지 않아요(안전함) | 읽기 | - | kg-model 13.3 |

비슷한 타입 찾기의 답은 `similar[]`(`type_id`, `reasons`: `name`, `attribute_overlap`, `same_dataset`, `same_standard`)와 점수가 아니라 이유의 목록이에요.

```json
POST /v1/type-similarity-checks
{"name": "Machine", "attributes": [{"name": "hostname", "value_type": "string"}], "datasets": ["ds_inventory"]}
→ {"data": {"similar": [{"type_id": "Server", "reasons": ["attribute_overlap", "same_dataset"]}]}, "meta": {...}}
```

표준 모델(`std`)의 타입은 읽기만 돼요. 오버레이(`type_overlay`)으로만 고쳐요(정의 3-1).

## 3. 수집
소스, 데이터셋, 피드 같은 설정은 정의의 항목이에요. 그래서 읽기만 있고, 고치는 경로는 4그룹의 브랜치 항목이에요. 운영의 조작(일시 정지, 지금 한 번)은 정의를 바꾸지 않아서 `operator`가 바로 해요. **브랜치에만 있는 소스**(새 소스는 병합 전에 브랜치에만 있어요)를 다루는 소스 탐색, 점검, 미리 보기, 올리기, 트라이얼 수집은 본문에 `branch_id`를 받아요(읽기는 `?branch_id`).

| 길 | 메서드 | 하는 일 | 역할 | 작업 | 원천 |
|---|---|---|---|---|---|
| `/v1/adapters` | GET | 어댑터 목록(범용 넷과 흐름 어댑터), 켜진 것 | 읽기 | - | ingestion |
| `/v1/presets`, `/v1/presets/{preset_id}` | GET | 프리셋: `version`, `adapter`, `allowed_requests`, `pagination`, `default_feeds`, `default_sensitivity`, `tests` | 읽기 | - | ingestion |
| `/v1/sources`, `/v1/sources/{source_id}` | GET | 소스: `adapter`, `preset`, `collector`, `contacts`, `protection`, `expected_interval`, `status`(`active`, `paused`, `breaker_open`). 비밀 값은 `credential_ref`(참조)만 | 읽기 | - | ingestion |
| `/v1/sources/{source_id}:introspect` | POST | 소스를 들여다봐요(어떤 표와 컬럼이 있나). 읽기 전용 요청만 나가요. `branch_id`를 받아요 | 아래 시크릿 규칙 | 작업 | ingestion 8.1 |
| `/v1/sources/{source_id}:check` | POST | 점검(연결, 인증, 읽기 전용의 세 겹이 확인되나). `branch_id`를 받아요 | 아래 시크릿 규칙 | 작업 | ingestion 8.1 |
| `/v1/sources/{source_id}/uploads`, `/v1/sources/{source_id}/upload-template` | POST, GET | 엑셀·CSV 소스의 파일 올리기와 칸 샘플 내려받기. 원본 레코드로 쌓여요. `branch_id`를 받아요 | 그 소스를 범위에 가진 `operator`나 `editor` | 작업 | ingestion 8.1 |
| `/v1/sources/{source_id}:pause`, `:resume` | POST | 일시 정지(`reason`을 받아요)와 다시 켜기. 헬스 상태에 보여요 | `operator` | - | ingestion 5 |
| `/v1/sources/{source_id}:push` | POST | 푸시 수집(사건의 길). [더한 기능의 경로](rest-api-resources-additions.md) 3절 | `ingester` | - | ingestion-extensions |
| `/v1/datasets`, `/v1/datasets/{dataset_id}` | GET | 데이터셋: `kind`, `receive_mode`, `natural_key`, `event_time`, `marker`, `deletion`, `columns`(`sensitivity`), `redact`, `expectations` | 읽기 | - | ingestion |
| `/v1/datasets/{dataset_id}:preview` | POST | 받아 둔 원본 레코드에서 샘플 줄과 컬럼마다의 통계(빈 값의 비율, 서로 다른 값의 수, 흔한 값, 값의 꼴)를 보여 줘요. 비밀 값은 지워지고 컬럼마다 민감 등급 한도를 따라 "가려짐"이 나오며, `open`이 아닌 컬럼의 흔한 값은 주지 않아요. `branch_id`를 받아요. 안전함. 샘플과 통계의 길은 이것 하나예요 | 그 소스를 범위에 가진 `editor`나 `operator`. 그 데이터셋이 대는 타입의 읽기 권한도 있어야 하고 감사에 남아요 | - | ingestion 7 |
| `/v1/refinements`, `/v1/refinements/{id}` | GET | 정제(닫힌 연산의 목록 `ops`, `inputs`) | 읽기 | - | ingestion |
| `/v1/feeds`, `/v1/feeds/{feed_id}` | GET | 피드: 데이터셋 하나에서 객체 타입 하나로 가는 매핑과 그 완전성 선언(`completeness_declaration`). 선언의 지금 상태는 `/v1/kgs/{kg}/completeness-declarations` | 읽기 | - | ingestion |
| `/v1/object-types/{type_id}/feed-table` | GET | **타입의 피드 표**: 그 타입의 속성마다 어느 소스의 어느 컬럼이 어떤 우선순위로 채우나, 근거의 종류, 받는 방식(배치와 흐름이 섞이면 둘 다) | 읽기 | - | ingestion, kg-model |
| `/v1/expectations`, `/v1/expectations/{id}` | GET | 기대 조건(받은 것이 지켜야 할 것. 정의의 항목). 액션 뒤의 기대와 대조는 다른 자원이에요(`/v1/kgs/{kg}/value-checks`) | 읽기 | - | ingestion |
| `/v1/schedules`, `/v1/schedules/{schedule_id}` | GET | 일정: `target`, `every`/`at`/`manual_only`, `windows`, `stream_enabled`, `reconcile_every`, `misfire_policy`, `expected_interval` | 읽기 | - | ingestion 5 |
| `/v1/schedules/{schedule_id}:run-now` | POST | 지금 한 번(같은 길, 같은 소스 보호, 같은 안전장치). 일정의 값은 안 바뀌어요 | `operator` | 작업 | ingestion 5 |
| `/v1/ingest/envelopes` | POST | **수집 엔벨로프를 받는 길.** 제품 밖의 수집기가 원본 레코드를 묶음으로 보내요(푸시 수집 `:push`는 사건 하나씩의 길이라 다른 길이에요). 본문은 엔벨로프(`envelope_version`, `capture_id`, `mode`, `capture_complete`, `redaction`, `body_hash`, `records`). `capture_id`가 멱등성 보장 키예요(같은 `capture_id`는 한 번만 받아요) | `ingester`(범위에 든 소스. 서비스 계정에 줘요) | - | ingestion 6 |
| `/v1/collectors`, `/v1/collectors/{collector_id}` | GET | 수집기: 마지막으로 보낸 때, 엔벨로프의 수, 빠진 `seq`, 버전 | 읽기 | - | ingestion 6.2 |

- 수집 엔벨로프를 받는 길은 `Idempotency-Key`가 아니라 엔벨로프의 `capture_id`와 `seq`로 겹침을 막아요(ingestion 6.1). 응답은 받은 수와 거절한 수, 데이터셋의 `redact`대로 다시 지운 수를 줘요. 크기가 상한을 넘으면 `invalid_input.envelope_too_large`예요.
- **시크릿을 복호화하는 일**(소스 탐색, 점검, 미리 보기, 트라이얼 수집)의 역할: 메인 브랜치의 소스 정의 그대로이면 그 소스를 범위에 가진 `editor`나 `operator`가 하고, 브랜치가 소스의 닿는 주소를 바꿨으면 그 소스의 `operator`여야 해요(ADR-0018). 닿는 주소를 바꾼 테스트 앞에는 바뀐 주소가 보여요.
- 정의의 `kind`가 `source`, `dataset`, `refinement`, `feed`, `identity_rule`, `exception_list`, `link_definition`, `expectation`, `schedule`인 것은 4그룹의 브랜치 항목으로 고쳐요.

```json
POST /v1/ingest/envelopes   Authorization: Bearer <`ingester` 시스템 계정 토큰>
{"envelope_version": "1", "source_id": "src_inventory", "dataset_id": "ds_servers", "capture_id": "cap_0007", "mode": "delta",
 "capture_complete": true, "expected_count": 2, "received_count": 2,
 "records": [{"kind": "added", "natural_key": "srv-0417", "seq": 41, "body": {"hostname": "srv-0417", "rack": "r-12"}},
             {"kind": "modified", "natural_key": "srv-0418", "seq": 42, "body": {"hostname": "srv-0418", "rack": "r-13"}}]}
→ 202 {"data": {"capture_id": "cap_0007", "accepted": 2, "rejected": 0, "redacted_again": 0}}
```

## 4. 실행
동기화의 실행, 흐름, 단계별 수는 읽기가 대부분이에요. 조작은 `operator`예요.

| 길 | 메서드 | 하는 일 | 역할 | 작업 | 원천 |
|---|---|---|---|---|---|
| `/v1/sync-runs` | GET | 실행 목록. 필터링: `source_id`, `dataset_id`, `status`, `started_at`(`range`), `guard_verdict` | 읽기 | - | data-observability 9.1 |
| `/v1/sync-runs/{run_id}` | GET | 실행 하나: `status`, `capture_range`, `definition_set`, `executor_version`, `diff_summary`, `guard_verdict`, `trace_id` | 읽기 | - | data-observability 9.1 |
| `/v1/sync-runs/{run_id}/stages` | GET | **단계별 수**(`run_stage`): `stage`, `in_count`, `out_count`, `dropped_count`, `dropped_by_reason`. 들어온 = 나간 + 떨어진 | 읽기 | - | data-observability 9.1 |
| `/v1/sync-runs/{run_id}/samples` | GET | **떨어진·떼어 둔 기록의 샘플.** 필터링: `stage`, `reason`. 내용은 읽을 때 원본 레코드에서 가져와요. 지워졌으면 "지워짐". 컬럼마다 민감 등급 한도로 "가려짐" | `operator`, `editor`, `admin`(그 소스의 범위) | - | data-observability 9.2 |
| `/v1/sync-runs/{run_id}/replays` | POST | **다시 돌리기.** 받아 둔 원본 레코드의 그 구간을 같은 정의 세트와 실행기로 뒤 단계만 다시 돌려 단계별 수와 떨어진 기록을 봐요. 소스에 안 가고 KG를 안 바꿔요. `body.branch_id`를 주면 브랜치의 정의로 돌려요 | `operator` | 작업 | data-observability 9.3 |
| `/v1/feed-flows` | GET | 흐름 목록: `flow_state`(`running`, `held`, `paused`, `failed`), `held_reason`, `held_since_seq`, `queued_changes`, `source_cursor`. 필터링: `source_id`, `flow_state` | 읽기 | - | kg-operations 4.2 |
| `/v1/feed-flows/{flow_id}` | GET | 흐름 하나 | 읽기 | - | kg-operations 4.2 |
| `/v1/feed-flows/{flow_id}:release` | POST | **풀기**: 쌓인 변경을 받은 순서대로 적용. 안전장치가 걸린 이유를 확인한 것으로 기록해요. 본문에 `acknowledged_reason`을 받아요 | `operator` | 작업 | kg-operations 4.2 |
| `/v1/feed-flows/{flow_id}:discard` | POST | **버리기**: 쌓인 변경을 적용하지 않고 지나가요(원본 레코드는 안 지워요). 맞추기를 할 수 있는 소스에서만 돼요. 아니면 `conflict.state_not_allowed` | `operator` | - | kg-operations 4.2 |
| `/v1/guard-confirmations` | POST | 안전장치에 걸린 배치 적용의 확인(처음 적용, 규칙을 고친 뒤 첫 적용, 스키마의 바뀜) | `operator` | - | ingestion 9 |

- 풀기 전에 드라이 런으로 차이를 봐요(위의 `replays`나 4그룹의 드라이 런). 붙잡힌 이유가 정의 탓이면 풀기 전에 정의를 고쳐 병합해요. 풀기는 `conflict.state_not_allowed`로 거절돼요(흐름이 `held`가 아닐 때).
- 실행과 흐름의 사건은 사건의 길(`stream_held`, `guard_blocked`)로도 와요.

## 5. 정의
정의의 읽기와 쓰기는 [정의](definitions.md) 14절의 이름이 길이 돼요. 모든 고치기는 브랜치 안에서 해요. 메인 브랜치를 바로 고치는 경로는 없어요. 항목은 `ETag`(판의 표시)를 가지고 바꿀 때 `If-Match`가 필요해요.

### 5-1. 묶음, 브랜치, 항목
| 길 | 메서드 | 하는 일 | 역할 | 작업 | 원천 |
|---|---|---|---|---|---|
| `/v1/definitions-overview` | GET | 메인 브랜치(지금 `set_no`, 이어짐), KG마다 쓰는 묶음과 메인 브랜치 끝의 어긋남(`kgs[]`의 `kg`, `applied_set_no`, `head_set_no`), 열린 브랜치와 제안(상태, 차단 사유, `overlaps_with`), 줄(순번과 맨 앞), 닫은 것 | 읽기 | - | definitions 14 |
| `/v1/definition-sets`, `/v1/definition-sets/{set_no}` | GET | 묶음 `D-N`의 목록과 항목 목록 | 읽기 | - | definitions 14 |
| `/v1/definition-sets/{from}/diff/{to}` | GET | 두 묶음의 차이(`definition_diff`) | 읽기 | - | definitions 14 |
| `/v1/definition-items/{item_id}` | GET | 한 항목의 판의 이력과 어느 제안이 바꿨나(`definition_item`) | 읽기(항목의 자리) | - | definitions 14 |
| `/v1/branches` | GET, POST | 브랜치 목록 / 열기(`base_set_no`가 바탕, 없으면 메인 브랜치의 끝) | `editor` | - | definitions |
| `/v1/branches/{branch_id}` | GET, DELETE | 브랜치(오버레이의 항목, 바탕, `content_hash`) / 닫기 | `editor` | - | definitions |
| `/v1/branches/{branch_id}/items` | GET, POST | 브랜치가 바꾼 항목 / 항목을 더하기(`kind`와 `body`) | `editor`(항목의 자리를 범위에 가진) | - | definitions 3 |
| `/v1/branches/{branch_id}/items/{item_id}` | GET, PATCH, DELETE | 항목을 읽고 고치고 삭제. 지우기는 "지움의 오버레이"이에요. `If-Match` 필요 | `editor` | - | definitions 3 |
| `/v1/branches/{branch_id}/items:batch` | POST | 여러 항목을 한 번에 쓰기(만들기, 고치기, 지우기의 목록. 항목마다 `If-Match`) | `editor`(항목의 자리를 범위에 가진) | - | definitions 3 |
| `/v1/branches/{branch_id}:align` | POST | 메인 브랜치 최신화(3-way 비교로 저절로 합치고 충돌은 남겨요). 맞추기의 길은 이것 하나예요 | `editor` | - | definitions 7 |
| `/v1/branches/{branch_id}/conflicts` | GET | 충돌의 목록(항목, 종류) | `editor` | - | definitions 7 |
| `/v1/branches/{branch_id}/conflicts/{conflict_id}:resolve` | POST | 충돌 풀기(내 것 / 메인 브랜치 / 손으로 고친 모습) | `editor` | - | definitions 7 |

```json
POST /v1/branches/br_12/items    Idempotency-Key: 31c9...
{"kind": "schedule", "body": {"target": {"dataset_id": "ds_servers"}, "every": "PT6H", "misfire_policy": "run_once"}}
→ 201 ETag: "rev-1"  {"data": {"item_id": "itm_88", "kind": "schedule", "branch_id": "br_12"}}
```

### 5-2. 제안, 검사, 승인, 줄
| 길 | 메서드 | 하는 일 | 역할 | 작업 | 원천 |
|---|---|---|---|---|---|
| `/v1/proposals` | GET, POST | 제안 목록(필터링: `status`, `blocker`, `author`, `item_kind`) / 브랜치에서 제안 만들기(`draft`) | `editor` | - | definitions 4 |
| `/v1/proposals/{proposal_id}` | GET | **제안**: 상태, 정의의 차이, 후보 KG의 차이, 충돌, 승인(필요한 것과 받은 것), 검사, **차단 사유**(`blockers`: `conflicts`, `stale`, `check_failed`, `check_pending`, `approvals_missing`, `guard_stopped`, `objective_missed`) | 읽기(항목의 자리) | - | definitions 4 |
| `/v1/proposals/{proposal_id}/checks` | GET | 검사의 결과: `check_kind`(`item_valid`, `merged_valid`, `item_tests`, `candidate_run`, `guard`, `competency`), `status`(`pending`, `passed`, `failed`, `needs_confirmation`), `subject_hash`, `inputs_digest`, `detail` | 읽기 | - | definitions 4-3 |
| `/v1/proposals/{proposal_id}/checks` | POST | 검사를 지금 돌려요(`check_kind`를 골라요). 초안 제안에서도 돌아서 큐가 돌릴 것을 미리 돌려 보는 경로가 이것이에요(주체마다 계산 예산이 걸려요) | `editor` | 작업 | definitions 4-3 |
| `/v1/proposals:from-items` | POST | **작은 변경을 제안 하나로**: 항목의 목록을 받아 브랜치를 열고 항목을 쓰고 초안 제안을 만들어요(`request_review: true`면 검토 요청까지). 항목마다 보통의 권한과 검사가 그대로 걸려요 | `editor`(항목의 자리를 범위에 가진) | - | definitions 3-2 |
| `/v1/proposals/{proposal_id}:request-review` | POST | `draft` → `in_review`. 낱개의 유효성 검사를 통과해야 해요 | `editor` | - | definitions 4-1 |
| `/v1/proposals/{proposal_id}/approvals` | GET | 필요한 승인(항목마다, 자리의 범위)과 받은 승인 | 읽기 | - | definitions 5 |
| `/v1/proposals/{proposal_id}:approve`, `:reject` | POST | 승인 / 거부. 현재 `content_hash`에 묶여요. 자기가 고친 것은 `permission.self_approval`로 거절 | `approver`(조직 표준 타입은 `model_steward`, 느슨하게 하는 제안은 `admin`) | - | definitions 5, access-control 10 |
| `/v1/proposals/{proposal_id}:enqueue` | POST | 줄에 세우기. 차단 사유가 있으면 `blocked.proposal` | `editor` | - | definitions 4-1 |
| `/v1/proposals/{proposal_id}:close` | POST | 닫기(`close_reason`: `abandoned`, `rejected`, `superseded`) | `editor`(고친 사람), `admin` | - | definitions 4-1 |
| `/v1/proposals/{proposal_id}/what-depends` | GET | 제안이 건드리는 것의 "무엇이 영향받나" | 읽기(정의의 자리) | - | definitions 12-4 |
| `/v1/merge-queue` | GET | 병합 큐(순번, 맨 앞, 막힌 것) | 읽기 | - | definitions 6 |

- 같은 승인 규칙을 쓰는 `approve`는 요청마다 `Idempotency-Key`를 받아요. 승인은 내용 해시에 묶여서, 내용이 바뀌면 `blocked.proposal`에 `approvals_missing`이 다시 떠요.
- 줄 맨 앞의 병합은 서버가 해요(사람이 부르는 길이 아니에요). 그 결과는 `proposal_state_changed`와 `definition_merged` 사건으로 와요.
- 작업(검사, 드라이 런, 트라이얼 수집)의 진행은 그 작업이 속한 제안을 읽을 수 있는 사람(승인자 포함)도 읽어요(rest-api.md 3-7).
- 안전장치에 걸린 드라이 런(`guard_stopped`)의 확인은 `/v1/guard-confirmations`(수집 그룹)로 해요. 확인은 브랜치의 내용 해시에 묶여요.

### 5-3. 드라이 런, 리버트, 계보, 내보내기와 들이기
| 길 | 메서드 | 하는 일 | 역할 | 작업 | 원천 |
|---|---|---|---|---|---|
| `/v1/proposals/{proposal_id}/candidate-runs` | POST | **드라이 런**: 받아 둔 원본 레코드로 후보 KG를 만들어요. 소스에 안 가요 | `editor` | 작업 | definitions 4-3 |
| `/v1/candidate-runs/{run_id}` | GET | 드라이 런의 결과: 정의의 차이와 KG의 차이를 함께, `inputs_digest`, `guard_verdict` | 읽기 | - | definitions 4-3 |
| `/v1/candidate-runs/{run_id}/diff` | GET | 후보 KG와 지금 KG 버전의 차이(타입별 더함·고침·물러남, 페이지네이션) | 읽기 | - | definitions 4-3 |
| `/v1/trial-ingests` | POST | **트라이얼 수집**: 새 소스를 브랜치에서 테스트 삼아 받아요(`branch_id`, `source_id`). 읽기 전용 요청만 | `operator`(시크릿 규칙은 3절) | 작업 | definitions 9 |
| `/v1/proposals/{proposal_id}/reverts` | POST | 병합된 제안을 되돌리는 새 브랜치와 제안(`origin: revert`). `item_ids`로 일부만, `emergency: true`로 급하게 | `editor`(급할 때는 `admin`이나 그 자리의 `approver`) | - | definitions 8 |
| `/v1/proposals/{proposal_id}/revert-preview` | GET | 리버트의 결과와 걸리는 것, 함께 되돌릴 목록 | 읽기 | - | definitions 8 |
| `/v1/definition-sets/{set_no}/restores` | POST | 묶음 `D-N`의 판으로 돌리는 새 제안(`origin: restore`). `scope`(소스, 객체 타입, 항목의 목록)를 받아요 | `editor` | - | definitions 8 |
| `/v1/definitions-lineage` | GET | 정의의 계보 그래프(항목에서 항목으로, 컬럼과 속성 단위). 입력은 시작 항목 | 읽기(정의의 자리) | - | definitions 12 |
| `/v1/definitions-lineage/what-depends` | POST | **누가 쓰나**: 대상(항목, 컬럼, 속성, 타입)과 하려는 일(고침, 물러남, 삭제)을 받아 `depends`, `readers`, `open_proposals`, `object_count`, `unreadable`을 줘요. 안전함 | 읽기 | - | definitions 12-4 |
| `/v1/definition-sets/{set_no}/exports` | POST | 묶음을 글 파일로 내보내요(정규 JSON, 항목 하나가 파일 하나, 비밀 값은 참조만). 자기 범위만큼 | `editor` | 작업 | definitions 13 |
| `/v1/definition-imports` | POST | 파일을 들여요. 언제나 새 브랜치와 제안(`origin: import`). 메인 브랜치를 덮어쓰지 않아요 | `editor` | - | definitions 13 |

- **리버트의 상태 전이.** 리버트는 새 제안이에요. 이후는 `draft` 이후의 보통의 흐름(검토, 승인, 줄)이에요. 긴급 리버트는 `admin`이나 그 제안이 고친 자리 전부를 범위에 가진 `approver`가 하고(`operator`는 못 해요), 감사 로그에 `admin_override`가 남고 사후 검토가 붙어요(`post_review_state: pending`).
- **타입 합치기와 올리기**는 브랜치 안의 도우미예요: `POST /v1/branches/{branch_id}/type-merges`(A를 B로), `POST /v1/branches/{branch_id}/type-promotions`(`ext`를 `org`로, `keep_ids`). 새 제안의 항목을 만들어요. `editor`가 제안하고 `model_steward`가 승인해요(ADR-0012). definitions 11.
- 묻는 쪽의 고정(`/v1/kgs/{kg}/serving-pins`)은 KG 무리에 있어요([다음 문서](rest-api-resources-graph.md)). 드라이 런이 만든 후보 KG는 `kg` 칸에 ID가 실리고 `/v1/kgs/{kg}/...`로 물어요.
- 내보낸 파일은 작업의 `result.href`로 받고 감사 로그에 남아요.
- 정의 항목마다의 정의의 테스트(`item_tests`)은 항목의 본문에 든 샘플이라 별도의 길이 없어요. 검사 길로 돌려요.

## 6. 그룹 안의 흐름 한 장

```
 브랜치 열기 ─ 항목 고치기(PATCH + If-Match) ─ 제안 만들기 ─ 검사(작업) ─ 후보 실행(작업)
      │                                                               │
      │                         (안전장치가 걸리면 guard-confirmations)│
      └────────────────── :request-review ─ :approve ─ :enqueue ───────┘
                                                              │ 줄 맨 앞에서 서버가 병합
                                          proposal_state_changed ─ definition_merged ─ kg_version_sealed
```

## 7. 알려진 한계와 물어 둘 것
- 소스 탐색·미리 보기·점검은 그 소스를 범위에 가진 `editor`나 `operator`(브랜치가 주소를 바꿨으면 `operator`), 트라이얼 수집은 `operator`로 정해졌어요.
- 엑셀 올리기는 그 소스를 범위에 가진 `operator`나 `editor`, 타입 쓰임 보고는 그 타입을 범위에 가진 `viewer`(`model_steward`는 전체), 타입의 합치기와 올리기는 `editor`가 제안하고 `model_steward`가 승인하는 것으로 정해졌어요. 겹치는 타입의 짝(`/v1/similar-type-pairs`)을 읽는 역할은 `model_steward`로 두었어요(정해진 것이 아니라 설계 담당 확인이 필요해요).
- 제안 만들기(`POST /v1/proposals`)가 `editor`인 것은 브랜치를 열고 고치는 것이 `editor`여서예요.
- 정의의 테스트(샘플)을 고치는 경로, 수집기의 만들기와 토큰 교체(서비스 계정 그룹)는 이 문서에 따로 적지 않았어요.
- 이 문서의 모든 목록의 거르는 칸은 표에 적은 것이 처음 후보예요. OpenAPI 문서에서 닫아요.

## 8. 관련 결정과 요구사항
- ADR-0016. 원천: ADR-0007(모델), ADR-0008(수집), ADR-0011(운영), ADR-0013(관측), ADR-0014(정의).
- REQ-143, REQ-147, REQ-148, REQ-149.
