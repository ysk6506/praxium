---
title: REST API 설계 — 계약이 먼저, 일관된 형식, 로그인, 계약을 지키는 장치, CLI
kind: guide
status: active
follows:
  - docs/design-docs/adr/0016-frontend-separate-rest-api-contract.md
---
# REST API 설계

화면은 다른 리포지토리에 있어요. 그래서 이 리포지토리의 백엔드와 화면은 **REST API의 계약(OpenAPI 문서) 하나로만** 만나요. 이 문서는 계약 전체에 적용되는 규칙을 적어요. 자원과 경로의 목록은 [모델·수집·실행·정의](rest-api-resources-data.md), [KG·관측·역량 질문·권한·기반](rest-api-resources-graph.md), [추가 기능의 경로](rest-api-resources-additions.md)에, 기능마다 경로가 있는지를 확인하는 표는 [기능별 경로](rest-api-feature-routes.md)에, 처음부터 끝까지 따라가는 예시 3개는 [예시 3개](rest-api-walkthroughs.md)에 있어요. 결정은 ADR-0016에 있어요.

> **계약은 이제 `backend/internal/contract/openapi/parts`의 조각으로 있고 병합한 `openapi.json`이 서버에 내장돼요(ADR-0024). 경로와 필드가 이 문서와 다르면 계약이 맞아요. 이 문서의 경로 중 알파에 있는 것은 24개이고 나머지는 `planned/routes.json`에 단계와 함께 있어요.** 이 문서는 OpenAPI 문서를 작성하는 사람이 규칙과 자원 목록을 혼동하지 않게 하려는 설계예요. 경로는 `/v1/...`로 적어요.

프런트엔드는 별도 리포지토리예요. 백엔드는 제품 수준으로 설계하고 구현하고, 화면은 알파에서 기능을 테스트할 수 있는 만큼(임시 콘솔)만 만들며 UX와 UI는 클로즈드 베타에서 제대로 해요.

## 1. 목적과 다루는 질문
- 화면 개발자가 이 문서만 보고 어떤 경로를 어떤 형식으로 호출하고, 무엇이 반환되고, 잘못 호출하면 무엇이 오는지 알 수 있어야 해요.
- 백엔드 구현자가 이 문서만 보고 OpenAPI 문서를 쓸 수 있어야 해요. 모든 경로가 같은 규칙을 따라야 하니까 규칙이 먼저예요.
- 인계 문서(from-terrarium) 6절의 쟁점 '사'에는 직접 답하지 않아요. 화면과 백엔드 사이의 경계를 정해요.

## 2. 개요

```
 화면(다른 리포지토리)  CLI 클라이언트        다른 시스템       에이전트
 계약에서 생성한 클라이언트  (API를 그대로 호출)  (서비스 계정)     (읽기 전용 MCP)
        │                     │                  │               │
        └──────── REST API: /v1/... (OpenAPI 계약) ──────────────┤ MCP 서버
                              │                                   │
                 ┌────────────┴───── 같은 유스케이스(use case) ─────┘
                 │  권한 판단 한 곳 · 응답 엔벨로프 · 사전 정의 질의 13종 · 정의 쓰기
                 └── 코어 ── 스토리지(PostgreSQL) · 질의 엔진 · 워커 · 수집기
```

| 항목 | 요약 |
|---|---|
| 계약 | OpenAPI 문서. 이 리포지토리에 있고 서버가 계약과 일치하는지 검사해요 |
| 버전 | 경로 맨 앞의 메이저 버전(`/v1`). 같은 메이저 버전 안에서는 추가만 해요 |
| 경로 구조 | KG에 속한 것은 `/v1/kgs/{kg}/...`(`main`은 운영 KG). 정의, 권한 부여, 시크릿, 운영은 `/v1/...` 바로 아래 |
| 형식 | 복수형 자원, JSON, `snake_case`, 페이지네이션은 `limit`과 `cursor`. 액션은 `POST /v1/.../{id}:액션` |
| 응답 | `data`와 `meta`. 엔벨로프는 사전 정의 질의의 엔벨로프와 같아요 |
| 쓰기 | 정의는 브랜치 안에서만. `Idempotency-Key`와 `ETag`/`If-Match` |
| 오래 걸리는 작업 | 202와 작업 자원 |
| 오류 | 표준 형식의 문제 상세 본문(JSON). 안정된 `code`. 메시지 문구는 화면이 만들어요 |
| 로그인 | 토큰을 검증만 해요. 세션이 없어요 |
| 자원 | 9개 그룹 |

## 3. 형식 규칙
각 규칙에 규칙, 이유, 짧은 예시를 적어요. 예시는 가상의 값이에요.

### 3-1. 이름과 값
**규칙.** 경로는 복수형 자원 이름이고 여러 단어는 하이픈으로 이어요(`/v1/object-types`). 필드 이름과 열거형 값은 `snake_case`예요. ID는 내부 구조를 해석하지 않는 불투명한 문자열이에요. 시각은 UTC 표준 표기(RFC 3339, `Z`로 끝남)예요. 경로의 하이픈 표기는 계약 품질 검사(5-3)가 확인해요. KG에 속한 것(객체, 링크, 질의, 변경, 버전, 충족률, 헬스 상태, 재수집)의 경로는 `/v1/kgs/{kg}/...`이고(3-15), 액션은 자원 뒤에 콜론으로 적어요(3-8).
**이유.** 이름이 경로마다 다르면 화면이 경로마다 예외 처리를 해야 해요. ID를 해석해서 쓰면 ID의 구조가 계약이 돼 버려요. 예: `{"object_id": "obj_7f3a", "created_at": "2031-04-02T09:30:00Z", "result": "degraded"}`

### 3-2. 응답 엔벨로프
**규칙.** 모든 성공 응답은 `data`와 `meta`를 가져요. `meta`의 필드는 사전 정의 질의의 엔벨로프(ADR-0010, [KG 질의](kg-queries.md))를 그대로 써요. 목록이 아닌 단순한 자원은 `meta`가 작아도 되지만 필드 이름은 같아요.
**이유.** KG에서 온 응답은 어느 시점까지 반영했는지, 얼마나 오래됐는지, 근거가 무엇인지, 잘렸는지를 항상 알려야 해요. REST와 MCP가 같아야 하고요(3-12).

| `meta`의 필드 | 의미 |
|---|---|
| `as_of` | `kg`(KG의 ID), `generation`(세대. 3-15), `change_seq`, `sealed_version`(있으면), 고정돼 있으면 `pinned`(고정한 버전과 이유), `volatile_as_of`(`current` 또는 `omitted`) |
| `served_by` | 응답한 역할(`api_sql`, `engine`) |
| `freshness` | 응답에 사용된 소스 중 가장 나쁜 값: `worst_source_id`, `oldest_age_seconds`, `state` |
| `evidence` | `min_grade`, `counts`(`fact`, `assumed`, `unknown`), `kinds`, `observation_age_seconds` |
| `result_complete` | `total`, `returned`, `truncated`, `reason`, `next_cursor` |
| `hidden` | 숨겨진 객체가 있는지 여부. 범위가 전체인 주체에게는 `hidden_passed_count`(경유한 수)와 `hidden_reached_count`(도달한 수)를 주고, 그 밖의 주체에게는 있음/없음만 줘요(설정으로 수를 공개할 수 있어요). 내용은 없어요 |
| `health_hint`, `model_limits`, `warnings` | 사용된 것 중 헬스 상태가 나쁜 것(ADR-0013), 이 응답이 알 수 없는 것, 경고 |
| `allowed_actions` | 자원 응답에서만. 지금 이 주체가 이 자원에 할 수 있는 액션(4절) |
| `trace_id` | 트레이스 ID(3-14). 헤더에도 실려요 |

```json
{"data": {"items": [{"object_id": "obj_7f3a", "object_type": "Server", "display_name": "srv-0417"}]},
 "meta": {"as_of": {"kg": "kg_main_01", "generation": 1, "change_seq": 88231, "sealed_version": "kv_0192", "pinned": null, "volatile_as_of": "current"},
          "served_by": {"role": "api_sql"}, "freshness": {"worst_source_id": "src_inventory", "oldest_age_seconds": 5400, "state": "fresh"},
          "evidence": {"min_grade": "assumed", "counts": {"fact": 11, "assumed": 1, "unknown": 0}},
          "result_complete": {"total": 12, "returned": 12, "truncated": false, "next_cursor": null},
          "hidden": {"has_hidden": false}, "trace_id": "tr_5c1e"}}
```

KG와 무관한 자원(예: 작업)의 `meta`에는 `as_of`와 `freshness` 같은 KG 필드가 없어요. 필드가 없는 것과 값이 비어 있는 것은 달라요. **수를 반환하는 모든 필드**(`total`, `counts_by_*`, `groups_touched`, `would_die_count`, `subtree_size`, `link_counts`, 충족률의 분모, `kg_changed`의 타입별 수 등)은 "숨겨진 것을 뺀 수"예요. 그래서 범위가 좁은 주체가 수의 차이로 다른 객체의 존재를 알아낼 수 없어요(ADR-0012).

### 3-3. 페이지네이션
**규칙.** 목록은 키 기반이에요. 요청에 `limit`(기본 100, 상한 1,000은 기본값(설정))과 `cursor`, 응답의 `meta.result_complete.next_cursor`가 다음 페이지의 키예요. 페이지 번호와 건너뛰기(offset)는 없어요. `cursor`는 불투명한 문자열이고 `kg`, `generation`, `change_seq`, 정렬 키(객체는 객체 ID이고 엔진의 내부 번호가 아니에요)를 포함해서, 페이지를 넘기는 도중에 변경이 들어와도 같은 위치에서 이어져요. 세대가 달라진 `cursor`는 `conflict.generation_changed`(409)로 거절해요(3-15). 큰 응답은 건수와 그룹별 요약을 먼저 주고(`shape=summary`가 기본) 개별 항목은 `shape=items`로 받아요.
**이유.** 변경이 계속 들어오는 KG에서 offset은 항목을 빠뜨리거나 겹쳐요.
```
GET /v1/kgs/main/issues?limit=2&filter[status][in]=open,in_progress
→ {"data": [{"issue_id": "iss_01"}, {"issue_id": "iss_02"}], "meta": {"result_complete": {"returned": 2, "next_cursor": "c_9aF2"}}}
GET /v1/kgs/main/issues?limit=2&cursor=c_9aF2&filter[status][in]=open,in_progress   (같은 필터링과 정렬을 다시 보내요)
```

### 3-4. 필터링, 정렬, 필드 선택 (고정 문법)
**규칙.** 필터링은 `filter[<필드>][<연산>]=<값>`이고, 연산은 `equals`, `in`, `prefix`, `range` 4가지뿐이에요(`find`의 연산과 같아요). `in`의 값은 쉼표로 구분하고, `range`는 `from..to`(한쪽을 비워도 돼요)예요. 정렬은 `sort=<필드>,-<필드>`(마이너스가 내림차순)이고 마지막에 ID가 붙어 순서가 확정돼요. 응답에 포함할 필드는 `fields=<필드>,<필드>`로 골라요. **자원마다 필터링할 수 있는 필드와 정렬할 수 있는 필드의 목록이 계약에 고정돼 있어요.** 목록 밖의 필드, 모르는 연산, 식(`and`/`or`)은 거절해요(`invalid_input.filter_field_unknown`, `invalid_input.filter_op_unknown`). 같은 필드에 필터를 둘 지정하면 AND로 적용해요. 숨겨진 속성으로 필터링하면 거절해요(ADR-0012).
**이유.** 열린 질의 언어로는 비용과 권한을 통제할 수 없어요. 고정 문법이면 계약이 모든 경우를 적을 수 있어요.

```
GET /v1/kgs/main/issues?filter[kind][equals]=duplicate_key&filter[status][in]=open,in_progress&filter[last_seen_version][prefix]=kv_01&sort=-last_seen_version&fields=issue_id,kind,status
```

### 3-5. 사전 정의 질의는 POST의 "질의" 자원
**규칙.** 사전 정의 질의 13종은 입력이 커서(객체 목록, 필터링, 범위) URL에 담을 수 없어요. 그래서 `POST /v1/kgs/{kg}/queries:<질의 이름>`으로 받아요. 이 POST는 상태를 바꾸지 않아요. 계약에 "안전함(`x-safe: true`)"이라고 표시하고(OpenAPI에는 이 표시가 없어 확장 필드로 둬요), `Idempotency-Key`를 받지 않아요. 부작용이 없어 다시 보내도 돼요. 간단한 `get`과 `find`는 GET도 제공해요. 응답은 3-2의 엔벨로프예요.
**이유.** URL은 길이 제한이 있고 로그에 남아요. 상태를 바꾸지 않는다는 것을 표시해야 클라이언트와 중간 장치가 재전송해도 안전한지 알 수 있어요.

```
POST /v1/kgs/main/queries:impact
{"dead": ["obj_sw0417"], "mode": "both", "view": "as_observed", "shape": "summary", "limit": 50}
→ 200 {"data": {"counts_by_result": {"down": 3, "degraded": 9}}, "meta": {...}}

GET /v1/kgs/main/objects/obj_7f3a?include=attributes,link_counts       (`get`)
GET /v1/kgs/main/objects?filter[object_type][equals]=Server&filter[display_name][prefix]=srv-04   (`find`)
```

### 3-6. 쓰기: 브랜치 안에서만, 멱등성 보장, 낙관적 동시성 제어
**규칙.**
- 정의를 수정하는 모든 작업은 브랜치 안에서 해요. 메인 브랜치를 직접 수정하는 경로는 없어요. 생성은 POST, 부분 수정은 PATCH(`application/merge-patch+json`, 보낸 필드만 바꿔요), 삭제는 DELETE예요. 여러 항목을 한 번에 쓰는 경로는 `POST /v1/branches/{branch_id}/items:batch`예요(한 `Idempotency-Key`, 항목마다 `If-Match`. 전부 성공하거나 전부 실패해요. 가정).
- **멱등성 보장.** 생성과 액션의 POST는 `Idempotency-Key`를 받아요. 클라이언트가 만든 고유한 문자열이에요. 같은 키와 같은 본문이면 처음 응답을 그대로 다시 줘요(`Idempotent-Replayed: true`). 같은 키에 다른 본문이면 거절해요(`conflict.idempotency_key_reused`). 같은 키의 요청이 아직 처리 중이면 `conflict.request_in_progress`예요. 키는 정해진 기간(가정) 보관해요. 키 없는 생성은 거절해요(`invalid_input.idempotency_key_required`).
- **낙관적 동시성 제어.** 항목을 읽으면 `ETag`(그 버전의 식별자, 항목의 버전 해시에서 만들어요)가 와요. PATCH, DELETE, 항목을 바꾸는 액션은 `If-Match`를 보내야 해요. 없으면 428(`invalid_input.precondition_required`), 일치하지 않으면 412(`conflict.version_mismatch`)예요. 412의 본문에 `current_etag`와 바뀐 필드가 있어 화면이 다시 읽고 병합한 결과를 보여 줄 수 있어요.
**이유.** 네트워크 때문에 같은 요청이 두 번 갈 수 있고, 두 사람이 같은 항목을 동시에 수정할 수 있어요. 조용히 덮어쓰면 "한 일은 모두 기록되고 되돌릴 수 있다"(ADR-0014)가 깨져요.

```
PATCH /v1/branches/br_12/items/itm_88        Idempotency-Key: 6e1f...   If-Match: "rev-17"
Content-Type: application/merge-patch+json
{"body": {"expected_interval": "PT6H"}}
→ 200 ETag: "rev-18"  {"data": {...}}
(이전 버전으로 다시 보내면) → 412 {"code": "conflict.version_mismatch", "current_etag": "rev-18", ...}
```

`ETag`가 일치하지 않으면 412, 없으면 428로 정했어요(AIP는 409를 쓰지만 이 설계는 HTTP의 일반 규칙을 따라요).

### 3-7. 오래 걸리는 작업: 202와 작업 자원
**규칙.** 드라이 런, 트라이얼 수집, 내보내기, 대량 읽기, 리플레이, 지정 재수집은 접수했다고 응답하고(202, `Location: /v1/jobs/{job_id}`) 작업 자원을 줘요. 작업은 상태, 진행률, 결과 위치, 취소를 가져요.

| 필드 (`job`) | 의미 |
|---|---|
| `job_id`, `kind` | `candidate_run`, `trial_ingest`, `definitions_export`, `bulk_read`, `replay`, `refresh` 등 |
| `scope_ref` | 작업이 속한 자원(`proposal_id`, `kg` 등). 없을 수 있어요 |
| `state` | `queued` / `running` / `succeeded` / `failed` / `cancel_requested` / `canceled` |
| `progress` | `stage`, `done`, `total`, `unit`. 전체를 모르면 `total`이 없어요 |
| `result` | 성공하면 `href`(결과를 받는 경로)와 `expires_at` |
| `error` | 실패하면 표준 오류 형식(3-9) |
| `created_by`, `created_at`, `started_at`, `finished_at` | 생성자와 각 시각 |
| `expires_at` | 이 시각에 작업과 결과가 삭제돼요. 보관 기간은 가정이고 7일부터 측정해서 정해요 |

```
queued ─▶ running ─▶ succeeded
   │         ├─────▶ failed
   └─▶ cancel_requested ─▶ canceled      (이미 끝난 작업은 취소 요청에 "이미 끝남"으로 응답해요)
```

작업의 상태와 진행률은 생성자, 같은 범위의 `admin`, 그리고 `scope_ref`가 가리키는 제안을 읽을 수 있는 사람이 조회해요(승인자가 드라이 런의 진행률을 봐요). 결과를 받는 경로(`result.href`)는 결과 자원의 읽기 권한을 따라요. 취소는 생성자만 할 수 있어요: `POST /v1/jobs/{job_id}:cancel`이에요. 같은 작업을 두 번 요청해도(멱등성 보장) 같은 작업을 돌려줘요. 결과가 크면 `result.href`가 페이지 단위 스트림이에요(3-13).

예: `POST /v1/proposals/prop_31/candidate-runs`가 `202`와 `Location: /v1/jobs/job_4d2`를 주고, `GET /v1/jobs/job_4d2`가 `state: running`과 `progress`를 줘요.

### 3-8. 상태를 바꾸는 액션
**규칙.** 상태를 바꾸는 액션은 자원 뒤에 콜론과 액션 이름을 붙여 POST해요(`POST /v1/proposals/{proposal_id}:approve`). **표기는 이것 하나예요.** 슬래시 뒤의 동사(`/approve`)는 쓰지 않아요. 새로 만들어지는 것이 자원이면 슬래시와 복수형 이름이에요(`/reverts`, `/candidate-runs`, `/exports`). 액션은 상태 전이가 허용하는 경우에만 할 수 있어요. 허용하지 않으면 `blocked.*`나 `conflict.state_not_allowed`로 거절하고 차단 사유 목록을 줘요.
**이유.** "제안을 승인한다"는 필드 하나를 PATCH하는 일이 아니에요. 승인 규칙, 검사, 감사 로그가 따라와요. 이름이 있어야 계약이 그 액션의 입력과 오류를 적을 수 있어요.

| 액션 | 경로 | 동작 |
|---|---|---|
| 검토 요청 | `POST /v1/proposals/{id}:request-review` | `draft` → `in_review` |
| 승인 / 거부 | `:approve`, `:reject` | 승인 규칙의 범위 안에서. 자기가 수정한 것은 승인할 수 없어요. 사람만 해요 |
| 병합 큐에 넣기 / 닫기 | `:enqueue`, `:close` | `approved` → `queued` / `close_reason`을 받아요 |
| 메인 브랜치 최신화 | `POST /v1/branches/{id}:align` | 충돌이 없으면 자동으로 돼요 |
| 리버트 | `POST /v1/proposals/{id}/reverts`, `POST /v1/definition-sets/{set_no}/restores`, `POST /v1/kgs/{kg}/identity-events/{id}:revert` | 새 브랜치와 제안을 만들어요 |
| 보류 해제 / 폐기 | `POST /v1/feed-flows/{id}:release`, `:discard` | `held`인 스트림. `operator` |
| 즉시 실행 | `POST /v1/schedules/{id}:run-now` | 작업을 만들어요 |
| 고정 | `POST /v1/kgs/{kg}/serving-pins`, `DELETE .../{pin_id}` | 질의 대상을 이전 KG 버전에 고정(전체 범위의 `operator`나 `admin`) |

저장하지 않고 미리 실행해 보는 액션(`:validate`, `:preview`, `:suggest`)은 안전해서 `Idempotency-Key`가 없어요. 목록은 [추가 기능의 경로](rest-api-resources-additions.md)에 있어요.

### 3-9. 오류
**규칙.** 오류는 표준 형식(RFC 9457 문제 상세 본문, `application/problem+json`)이에요. 필드: `type`(오류 `code`를 설명하는 경로), `title`(참고용 영어 한 줄), `status`, `code`(안정된 이름), `params`(화면이 메시지를 만들 때 쓰는 값), `trace_id`, `errors`(어느 필드가 잘못됐는지). **메시지 문구는 화면이 `code`와 `params`로 만들어요.** `title`은 사람이 로그를 읽을 때만 써요. 권한이 없어 안 보이는 것과 없는 것을 구분하지 않아요. 둘 다 404 `not_found.resource`예요.
`code`는 `<그룹>.<세부>`예요. 한번 발행한 `code`의 의미는 바꾸지 않고, 새 `code`를 추가하는 것은 같은 메이저 버전 안에서 할 수 있어요. 목록은 계약에 고정돼 있어요.

| 그룹 | 상태 | 의미 | 예 |
|---|---|---|---|
| `auth` | 401 | 신원을 신뢰할 수 없어요 | `auth.token_missing`, `auth.token_expired`, `auth.token_invalid` |
| `permission` | 403 | 보이는 대상이지만 그 작업은 할 수 없어요 | `permission.role_missing`, `permission.not_in_scope`, `permission.self_approval` |
| `not_found` | 404 | 없거나 보이지 않아요 | `not_found.resource` |
| `invalid_input` | 400, 422, 428 | 입력이 틀려요 | `invalid_input.field`, `invalid_input.filter_field_unknown`, `invalid_input.precondition_required` |
| `conflict` | 409, 412 | 현재 상태와 일치하지 않아요 | `conflict.version_mismatch`, `conflict.state_not_allowed`, `conflict.request_in_progress`, `conflict.generation_changed` |
| `rate_limited` | 429 | 한도를 초과했어요 | `rate_limited.requests`, `rate_limited.concurrency`, `rate_limited.compute_budget` |
| `blocked` | 409 | 차단 사유가 있어요(`blockers`에 사유 목록) | `blocked.proposal` |
| `stale` | 409, 410, 503 | 기준 데이터가 오래됐거나 아직 준비되지 않았어요 | `stale.branch_behind_trunk`, `stale.since_too_old`, `stale.projection_not_ready`, `stale.min_seq_not_reached` |
| `server` | 500, 503 | 서버 내부 문제 | `server.internal`, `server.unavailable` |

**필드별 오류.** 입력이 잘못되면 `errors`에 필드마다 하나씩 줘요. `pointer`는 JSON 포인터(표준)예요.

예 1. 잘못된 입력.
```json
{"type": "/v1/problems/invalid_input.field", "title": "Invalid input", "status": 422, "code": "invalid_input.field", "trace_id": "tr_a1",
 "errors": [{"pointer": "/body/expected_interval", "code": "invalid_input.field_format", "params": {"expected": "iso8601_duration"}},
            {"pointer": "/name", "code": "invalid_input.field_required", "params": {}}]}
```
예 2. 보이지 않는 대상(없는 대상과 같아요).
```json
{"type": "/v1/problems/not_found.resource", "title": "Not found", "status": 404, "code": "not_found.resource", "params": {"resource": "object"}, "trace_id": "tr_a2"}
```
예 3. 차단 사유.
```json
{"type": "/v1/problems/blocked.proposal", "title": "Proposal is blocked", "status": 409, "code": "blocked.proposal", "trace_id": "tr_a3",
 "blockers": [{"blocker": "approvals_missing", "params": {"item_id": "itm_88", "scope": "source:src_inventory"}},
              {"blocker": "check_failed", "params": {"check_kind": "merged_valid", "item_id": "itm_91"}}]}
```

### 3-10. 이벤트 구독: 이벤트 스트림과 `since`
**규칙.** 화면이 진행 상황과 변경을 볼 수 있도록 경로를 두 가지 제공해요. 같은 이벤트를 다른 방식으로 수신하는 거예요.
- `GET /v1/events/stream`: 서버가 보내는 이벤트 스트림(`text/event-stream`, SSE). 이벤트마다 `id`(`event_id`)가 붙고 KG 이벤트의 `event_id`는 세대를 포함해요. 연결이 끊겼다가 다시 붙을 때 마지막 `event_id`를 `Last-Event-ID` 헤더로 보내면 그 이후부터 수신해요.
- `GET /v1/events?since=<event_id>`: 같은 이벤트를 번호로 조회하는 경로예요(페이지네이션). 놓친 이벤트를 보충해요. 가진 `event_id`가 너무 오래됐으면 `stale.since_too_old`이고, 시점 복구로 세대가 바뀐 뒤의 `event_id`이면 `conflict.generation_changed`예요. 둘 다 화면은 상태를 다시 읽어야 해요. 이벤트를 보관하는 기간은 가정이에요.

둘 다 `kinds`, `resource`(자원 종류와 ID), `kg`(KG 이벤트만)로 필터링해요. **읽기 권한이 이벤트에도 적용돼요.** 이벤트는 내용이 아니라 알림이에요. 화면이 알림을 받고 자원을 다시 읽어요.

| 이벤트의 `kind` | 그룹 | 담는 내용 |
|---|---|---|
| `job_progress`, `job_finished` | 기반 | `job_id`, `state`, `progress` |
| `proposal_state_changed` | 정의 | `proposal_id`, 앞뒤 상태, 차단 사유의 수 |
| `definition_merged`, `definition_reverted`, `kg_version_sealed` | 정의, KG | `set_no`나 `sealed_version` |
| `kg_changed` | KG | `kg`, `generation`, `change_seq`의 구간, 타입별 수(숨겨진 것을 뺀 수). 경량 변경(`volatile`)도 포함해요. 내용은 없어요(내용은 `changes`로 읽어요) |
| `health_changed`, `alert_opened`, `alert_resolved` | 관측 | 자원, 상태 |
| `guard_blocked`, `stream_held` | 실행 | 소스, 데이터셋 |
| `serving_pin_changed` | KG | `pin_id`, 고정 또는 해제 |

```
id: ev_000812
event: proposal_state_changed
data: {"proposal_id": "prop_31", "from": "in_review", "to": "approved", "blocker_count": 0}
```

서버는 연결 유지를 위해 주기적으로 빈 줄을 보내요. 토큰은 요청 헤더(`Authorization`)에 넣고 URL에는 넣지 않아요. 표준 `EventSource`는 요청 헤더를 설정할 수 없어서, 클라이언트는 헤더를 설정할 수 있는 방식(예: fetch로 스트림 읽기)을 써요.

### 3-11. 한도와 헤더
**규칙.** 호출자(계정)별 한도(초당 요청 수, 동시 요청 수, 계산 예산. ADR-0010)와 남은 양을 모든 응답에 포함해요. `RateLimit-Policy`와 `RateLimit` 헤더(IETF 초안의 형식)를 써요. 초과하면 429와 `Retry-After`(초)로 응답하고 본문에 `rate_limited.*`와 `retry_after_seconds`, 요청 범위를 줄이는 방법을 줘요. 한도 수치는 측정해서 정해요.
**이유.** 사전 정의 질의에는 비용 등급이 있어요. 한도를 숨기면 화면이 무엇을 줄여야 하는지 알 수 없어요. 이 헤더는 아직 초안이라 이름이 바뀔 수 있어요(6절).

### 3-12. REST와 MCP는 같은 유스케이스 위의 두 어댑터
권한 판단, 응답 엔벨로프, 오류 `code`가 둘 다 같아요. 대응표는 5-6이에요.

### 3-13. 대량 읽기와 내보내기
**규칙.** 범위 하나를 한 KG 버전에서 통째로 읽는 `bulk_read`와 정의 세트의 내보내기는 작업(3-7)이에요. 성공하면 `result.href`에서 페이지 단위로 내려받아요. 한 줄에 JSON 하나인 스트림(`application/x-ndjson`)이고 첫 줄이 엔벨로프(`meta`)예요. 중간에 끊겨도 `cursor`로 이어서 받아요. 숨김 규칙이 적용돼요. `bulk_read`는 `viewer`에게 별도 허가가 있어야 하고 사용한 사람이 감사 로그에 남아요(ADR-0012).

```
POST /v1/kgs/main/scopes/scp_servers/bulk-reads   {"kg_version_id": "kv_0192"}   → 202 job_9a
GET  /v1/jobs/job_9a/result?cursor=...   → {"meta": {...}}\n{"object_id": ...}\n{"object_id": ...}\n
```

### 3-14. 트레이스 ID
**규칙.** 모든 응답은 `X-Trace-Id` 헤더와 `meta.trace_id`(오류는 `trace_id`)를 포함해요. 요청에 W3C의 `traceparent`가 있으면 이어받아요(가정). 작업과 동기화 실행 이력의 `trace_id`와 같은 ID예요(ADR-0013).

### 3-15. KG는 계약의 자원이고 세대가 따라다녀요
**규칙.**
- KG는 자원이에요(`GET /v1/kgs`, `GET /v1/kgs/{kg}`). `{kg}`는 KG의 ID이고 운영 KG는 고정된 이름 `main`으로도 가리켜요. KG에 속한 것을 다루는 모든 경로는 `/v1/kgs/{kg}/...` 아래예요: 객체, 링크, 사전 정의 질의 13종, 변경, 버전, 충족률, 헬스 상태, 문제 목록, 재수집, 기대와 대조, 식별 이벤트, 저장해 둔 범위, 고정. 후보 KG, 테스트 KG, 시스템 KG도 같은 경로로 질의해요.
- 정의(타입, 소스, 데이터셋, 피드, 브랜치, 제안), 권한 부여, 시크릿, 운영은 배포에 하나만 있어서 `/v1/...` 바로 아래예요. 정의의 메인 브랜치는 배포에 하나이고 KG는 그 메인 브랜치의 어느 정의 세트를 쓰는지 가리켜요(시스템 KG는 `sys` 네임스페이스의 항목만 써요).
- 권한 부여의 범위에 KG를 지정하는 필드가 있어요(기본은 `main`).
- **세대**(`generation`)는 시점 복구를 할 때마다 오르는 번호예요. `as_of`, `cursor`, 이벤트의 `event_id`, `min_seq`에 포함돼요. 세대가 다르면 `conflict.generation_changed`로 거절하고, 화면은 상태를 다시 읽어요.
- **`min_seq`와 `wait_timeout`.** 질의하는 쪽이 `min_seq`(적어도 이 변경 번호까지 반영한 응답)와 `wait_timeout`을 줄 수 있어요. 복제본이 해당 번호에 도달하지 못했으면 주 스토리지로 질의하거나 `wait_timeout`까지 기다리고, 그래도 도달하지 못하면 `stale.min_seq_not_reached`로 거절해요. **`max_staleness`는 이것과 무관해요.** `max_staleness`는 소스에서 읽은 값이 얼마나 오래돼도 되는지만 뜻해요.
- 큰 배치는 `change_seq` 여러 개로 나뉘어 적용되지만, 읽는 쪽은 배치가 끝날 때만 오르는 읽기 워터마크(`readable_through`)까지만 봐요. `min_seq`와 고정이 이 워터마크를 따라요.
- "그 버전 시점"의 응답에는 `volatile` 값을 주지 않아요(현재 값을 섞지 않아요). 엔벨로프의 `volatile_as_of: omitted`가 그것을 알려요.
**이유.** `/v1`은 같은 메이저 버전 안에서 추가만 해요. KG를 지정하는 부분을 나중에 경로에 넣으면 호환성을 깨는 변경이라 처음부터 경로에 둬요. "KG 분리"는 쓰기 한계를 해소하는 수단이 아니에요. 영향 범위 계산이 KG 하나 안에서만 이뤄져서, 서로 의존하지 않는 자산 그룹만 다른 KG가 돼요(ADR-0009).

## 4. 로그인과 권한

백엔드는 **토큰을 검증하는 쪽**이에요. 세션을 보유하지 않아요.

```
화면            SSO(OIDC)                 프락시움 API
 │ 1 인증 요청(인가 코드 플로우 + PKCE의 challenge) │
 ├──────────────▶│                            │
 │ 2 사용자가 SSO에서 로그인                   │
 │◀──────────────┤ 3 인증 코드                 │
 │ 4 코드 + PKCE의 verifier로 토큰 교환        │
 ├──────────────▶│                            │
 │◀──────────────┤ 5 토큰(access token)        │
 │ 6 Authorization: Bearer <토큰>              │
 ├───────────────────────────────────────────▶│ 7 서명·발급자·대상·만료 검증
 │◀───────────────────────────────────────────┤ 8 권한 판단(한 곳) 후 응답
```

- **토큰 검증.** 서명(SSO가 공개한 키), 발급자, 대상(이 API), 만료를 검증해요. 사용자와 그룹 정보는 SSO가 갖고 있고 프락시움은 거기에 역할과 범위를 부여해요(ADR-0012). 만료되면 `auth.token_expired`예요. 토큰 재발급은 화면과 SSO 사이의 일이에요.
- **서비스 계정.** 프락시움이 발급한 토큰을 `Authorization: Bearer`로 보내요. 토큰은 생성할 때 한 번만 보이고 해시만 저장돼요(ADR-0012). 수집에 쓰는 계정은 `ingester` 역할이에요(푸시 수집과 수집 엔벨로프만 할 수 있고, 읽기 권한도 없어요).
- **CLI의 사용자 로그인.** 디바이스 인가 플로우(OAuth 표준)를 써요. 사용자는 다른 기기의 브라우저에서 SSO로 로그인하고 CLI는 토큰을 받아요. 서비스 계정은 위의 토큰을 써요.
- **허용 출처(CORS).** 화면이 다른 출처에서 제공되므로 허용할 출처 목록을 설정으로 받아요. 기본은 빈 목록(전부 차단)이고, 목록에 없는 출처에는 CORS 허용 헤더를 주지 않아요.
- **"나" 경로.** `GET /v1/me`가 현재 사용자가 누구이고 무엇을 할 수 있는지를 줘요.

```json
{"data": {"subject": {"kind": "person", "subject_id": "sub_21", "display_name": "아무개"},
          "grants": [{"grant_id": "gr_5", "role": "editor", "scope": {"kg": "main", "object_types": ["Server"], "sources": ["src_inventory"]}, "expires_at": null}],
          "bulk_read": false,
          "allowed_actions": [{"action": "propose", "allowed": true, "reason_code": null}, {"action": "grant", "allowed": false, "reason_code": "forbidden.role"}],
          "capabilities": {"api_version": "v1", "features": ["competency", "events_stream"]}}}
```

- **자원별 가능한 액션 안내.** 자원을 읽는 응답의 `meta.allowed_actions`는 `[{action, allowed, reason_code}]`예요. `action`은 액션 이름(3-8의 `:` 뒤의 이름이나 `edit` 같은 것)이고, 차단됐으면 `allowed: false`와 `reason_code`가 와요. `reason_code`는 고정 목록이고 `/v1/catalog/reason-codes`가 줘요.

| `reason_code` | 의미 |
|---|---|
| `forbidden.role` | 필요한 역할이 없어요 |
| `forbidden.scope` | 역할은 있지만 이 자원이 범위 밖이에요 |
| `conflict.state` | 자원의 현재 상태가 그 액션을 허용하지 않아요 |
| `blocked.approval_required` | 승인이 먼저 필요해요 |
| `blocked.limit` | 한도(주체별 열린 건수나 예산)에 도달했어요 |
| `unavailable.degraded` | 시스템이 이상 상태라서 지금은 할 수 없어요 |

  화면이 버튼을 활성화하거나 비활성화하고 이유를 보여 주는 데 써요. **참고용일 뿐이에요.** 판단은 언제나 서버가 하고, 버튼이 활성화돼 있어도 서버는 요청마다 다시 판단해 `permission.*`로 거절할 수 있어요.
- **역할.** 경로마다 필요한 역할은 자원 목록 표에 적어요. 역할 이름은 [권한 설계](access-control.md)의 8종(`viewer`, `editor`, `approver`, `operator`, `model_steward`, `auditor`, `admin`, `ingester`)이에요.

## 5. 계약을 지키는 장치

### 5-1. OpenAPI가 진실이다
계약은 OpenAPI 문서이고 이 리포지토리에 있어요(위치는 다음 변경에서 정해요). **계약을 먼저 쓰고** 서버가 거기에 맞춰요. 코드에서 계약을 추출하지 않아요. 문서 버전은 OpenAPI 3.1로 시작해요(3.2가 나왔어요. 올릴 때는 6절). 계약을 쓰기 전에 [기능별 경로](rest-api-feature-routes.md)의 표에 빈 항목이 없어야 하고, 이 표를 계약 검사와 연결해요(5-3).

### 5-2. 서버와 계약의 일치, 호환성을 깨는 변경
| 검사 | 보는 것 | 실패하는 때 |
|---|---|---|
| 서버와 계약의 일치 | 계약에 있는 경로가 서버에 있고 그 반대도 성립하는지, 응답이 계약의 형식인지 | 계약에 없는 경로가 서버에 있거나 그 반대 |
| 호환성을 깨는 변경 | 이전 메이저 버전의 계약과 비교해요: 경로나 필드 삭제, 필드의 이름이나 형식 변경, 선택 필드를 필수로 변경, 열거형 값 삭제, 응답의 오류 `code` 의미 변경 | 같은 메이저 버전 안에서 호환성을 깨는 변경 |
| 계약의 품질 | 5-3 | 규칙 위반 |

검사 도구는 다음 변경에서 선정해요.

### 5-3. 계약의 품질 검사
- 이름 규칙(경로는 복수형과 하이픈, 필드와 열거형 값은 `snake_case`).
- **모든 응답에 예시**가 있어요. 오류 응답도 마찬가지예요.
- **모든 오류 `code`의 목록**이 계약에 있고 응답이 그 안의 것만 써요.
- 페이지네이션 형식(`limit`, `cursor`, `meta.result_complete`)과 오류 형식(문제 상세 본문)이 모든 경로에서 같아요.
- 사전 정의 질의에 "안전함" 표시가 있고 생성과 액션의 POST에 `Idempotency-Key`가, 수정 경로에 `If-Match`가 있어요.
- 모든 경로에 필요한 역할(`x-required-role`), 상태를 바꾸지 않는 경로의 표시(`x-safe`), 오래 걸리는 작업의 표시(`x-long-running`)가 있어요. 목록의 `meta.result_complete`는 `total`, `returned`, `truncated`, `reason`, `next_cursor`를 모두 줘요. 커서는 모델 해시(세대)를 담고 있어서 잘못되면 `invalid_input.cursor_invalid`, 세대가 달라지면 `conflict.generation_changed`예요.
- **문서의 모든 기능에 `/v1` 경로가 있어요.** [기능별 경로](rest-api-feature-routes.md)의 표가 그 검사의 기준이에요. 액션은 콜론 표기만 써요.
- KG에 속한 경로는 모두 `/v1/kgs/{kg}/...` 아래에 있어요.

### 5-4. 버전, 지원 중단, 화면이 계약을 사용하는 방법
- 메이저 버전은 경로 맨 앞(`/v1`)이에요. 같은 메이저 버전 안에서는 추가만 해요: 새 경로, 새 선택 필드, 새 응답 필드, 새 오류 `code`. 화면은 모르는 응답 필드를 무시해야 해요(수신 측의 규칙).
- 호환성을 깨는 변경은 새 메이저 버전(`/v2`)으로 배포해요. 이전 버전은 지원 중단을 공지한 뒤 정해진 기간 동안 함께 제공해요. 지원 중단은 계약의 `deprecated` 표시와 응답의 `Deprecation`, `Sunset` 헤더(둘 다 IETF 표준)로 알려요. 기간은 정하지 않았어요(7절).
- **프런트엔드 리포지토리는 계약의 특정 버전(태그나 해시)을 고정해서** 거기서 클라이언트를 생성해 써요. 계약이 바뀌면 프런트엔드 리포지토리가 버전을 올리고 생성된 클라이언트의 차이를 확인해요. 모르는 메이저 버전은 쓰지 않아요.
- `GET /v1/contract`가 계약 문서를, `GET /v1/capabilities`가 서버의 버전과 활성화된 기능을 줘요.

### 5-5. CLI
- **API를 그대로 호출해요.** 서버 안의 코드에 직접 접근하지 않아요. 그래서 화면 없이도 모든 기능을 테스트하고 운영할 수 있어요. 알파의 기준은 "모든 기능이 API와 CLI로 된다"(REQ-147)예요.
- **명령 형식은 자원과 액션을 따라가요**: `<그룹> <자원> <액션> [인자]`. 도구 이름은 `praxium`이고 서버와 같은 바이너리에 있어요. 서버는 `praxium api serve`로 기동하고, 클라이언트 플래그(`--endpoint`, `--token`, `--config`, `--json`)는 명령 앞에 놓아요. 종료 코드는 오류 그룹을 따라요(ADR-0024).

```
praxium question impact --kg main --dead obj_sw0417 --view as_observed
praxium proposal approve prop_31
praxium job get job_4d2 --wait            # 끝날 때까지 기다려요
```

- 출력은 기본이 JSON이고 `--output table`도 있어요. 종료 코드는 오류 그룹을 따라가요. 로그인은 사용자면 디바이스 인가 플로우(4절), 시스템이면 서비스 계정의 토큰이에요.

### 5-6. MCP와의 대응
MCP 서버는 같은 유스케이스 위의 얇은 계층이에요. **KG의 MCP는 읽기만 해요.** KG를 수정하는 경로가 없고 툴 이름은 사전 정의 질의의 이름과 같아요. 툴은 `kg`(기본 `main`)를 받아요. 지정 재수집과 기대와 대조도 읽기 인터페이스로 제공해요(ADR-0010). 재수집은 작업을 만들지만 현실 시스템에 쓰지 않고, 소스를 읽어 일반 경로로 적용하는 읽기이기 때문이에요(KG는 원본 레코드의 결과예요). 운영 MCP 인터페이스는 따로 있고 액션이 있어요([프락시움 자신의 관측성](self-observability.md)).

| 사전 정의 질의 | REST | MCP 툴 | 차이점 |
|---|---|---|---|
| `find` | `GET /v1/kgs/{kg}/objects`, `POST /v1/kgs/{kg}/queries:find` | `find` | |
| `get` | `GET /v1/kgs/{kg}/objects/{object_id}` | `get` | |
| `neighbors`, `containment`, `paths`, `impact`, `exposure` | `POST /v1/kgs/{kg}/queries:<이름>` | 같은 이름 | |
| `changes`, `versions` | `POST /v1/kgs/{kg}/queries:<이름>` | 같은 이름 | REST는 `GET /v1/kgs/{kg}/changes`도 줘요 |
| `quality`, `health` | `POST /v1/kgs/{kg}/queries:<이름>` | 같은 이름 | |
| `scopes` | `GET /v1/kgs/{kg}/scopes` | `scopes` | 범위를 만드는 경로는 REST만 |
| `competency` | `POST /v1/kgs/{kg}/queries:competency` | `competency` | |
| 재수집 | `POST /v1/kgs/{kg}/refreshes` | `refresh` | 작업을 줘요 |
| 기대와 대조 | `POST /v1/kgs/{kg}/value-checks` | `check_values` | |
| `bulk_read` | `POST /v1/kgs/{kg}/scopes/{scope_id}/bulk-reads` | 없음 | 에이전트에게 제공하지 않아요 |
| 정의의 읽기 | `GET /v1/definitions-overview` 등 | `definitions_overview`, `proposal`, `what_depends` 등 | 쓰기는 REST만 |

- 입력의 `shape`는 REST에서 `summary`/`items`이고 MCP에서는 `detail`로 `brief`/`full`을 골라요(ADR-0010). 오류는 둘 다 같은 `code`를 주고 MCP는 다음에 할 일을 추가로 알려 줘요.

## 6. 표준과의 비교
설계를 표준 용어에 맞추고, 빠졌거나 어긋나는 곳을 적어요. 결정을 바꾸지는 않아요. 확인한 것과 가정을 구분해 적어요.

| 주제 | 표준 현황 | 이 설계 |
|---|---|---|
| 오류 형식 | RFC 9457(문제 상세)이 이전 표준을 대체했어요 | 따라요(3-9) |
| 지원 중단 헤더 | `Deprecation`과 `Sunset`은 IETF 표준이에요 | 따라요(5-4) |
| `Idempotency-Key` | IETF 초안이에요(draft-ietf-httpapi-idempotency-key-header-08, 표준이 아님). 재전송 규칙과 오류 상태가 초안에 있어요 | 초안의 의미를 따라요. 이름이 바뀔 수 있어요 |
| 한도 헤더 | `RateLimit`와 `RateLimit-Policy`도 IETF 초안이에요(draft-ietf-httpapi-ratelimit-headers-11) | 초안을 따라요. 바뀔 수 있어요 |
| 상태를 바꾸지 않는 큰 요청 | 본문을 가진 안전한 메서드(HTTP QUERY 메서드)가 제안돼 있고 OpenAPI 3.2에 `query` 작업이 있어요. 번호와 상태는 확인하지 못했어요 | 결정은 POST의 질의 자원이에요. `QUERY`가 널리 쓰이면 같은 자원에 `QUERY`를 추가하는 것은 추가 변경이에요. 7절 |
| 페이지네이션과 오래 걸리는 작업 | Google의 API 개선 제안(AIP-158은 불투명한 페이지 토큰, AIP-151은 장기 실행 작업)과 Zalando 지침(#160은 키 기반 페이지네이션 권장)이 권해요. Microsoft의 지침은 확인하지 못함 | 따라요 |
| 보이지 않는 대상 | AIP-193은 권한을 먼저 확인하고 있든 없든 403을 주라고 해요 | 결정은 보이지 않는 대상은 404(있음과 없음을 구분하지 않음). 같은 목적이고 상태 코드만 달라요. 7절 |
| 경로의 버전 | Zalando 지침(#115)은 경로에 버전을 넣지 말라고 해요. Azure는 `api-version` 쿼리 파라미터를 써요 | 결정은 경로의 메이저 버전이에요 |
| 브라우저 앱의 토큰 | 브라우저 앱의 OAuth 모범 사례는 업무용 앱에 BFF를 강하게 권해요(번호는 확인하지 못했어요). 인가 코드 플로우와 PKCE, 디바이스 인가 플로우는 OAuth 표준이에요 | 결정은 화면이 토큰을 보유하고 BFF를 두지 않아요. 7절 |
| 서버 전송 이벤트(SSE) | WHATWG 표준. `EventSource`는 요청 헤더를 설정할 수 없어요 | 헤더를 설정할 수 있는 방식을 써요(3-10) |
| OpenAPI | 3.1이 널리 쓰이고 3.2(최신 3.2.1)가 나왔어요 | 3.1로 시작해요(가정). 도구가 3.2를 따라오면 올려요 |

`If-Match` 실패는 HTTP의 일반 규칙대로 412예요(AIP는 409). 결정이에요.

## 7. 알려진 한계와 미결 사항
**한계(가정).**
- 페이지 크기(기본 100, 상한 1,000은 기본값 표의 값), 멱등성 보장 키를 보관하는 기간, 작업과 이벤트를 보관하는 기간, 한도 수치는 가정이고 측정해서 정해요. 푸시 수집의 중복 제거 기간은 계약에 적고 기본이 7일이에요.
- 이 문서의 경로와 필드는 OpenAPI 문서에서 확정돼요. 그때까지는 경로 이름과 필드 수가 바뀔 수 있어요.
- 추가만 하는 규칙은 검사가 지켜요. 검사가 모든 호환성을 깨는 변경을 잡는다는 보장은 없어요.

**미결 사항.**
- 경로에 메이저 버전을 두는 것과 별개로, 호환성을 깨지 않는 선택 기능(활성화된 기능)을 알리는 방법의 세부(`capabilities`의 필드).
- 지원이 중단된 메이저 버전을 함께 제공하는 기간은 설정이고 기본값은 12개월이에요([기본값 표](defaults.md)).
- 보이지 않는 대상과 없는 대상을 404로 같게 처리하는 규칙, 그리고 쓰기 권한 부족(`permission.*`)이 존재 여부를 알려 주는 정도. 읽을 수 있는 자원에만 `permission.*`를 주는 것으로 시작해요.
- `QUERY` 메서드와 OpenAPI 3.2로 갈지, 화면의 `BFF`가 필요한지(필요하면 프런트엔드 리포지토리가 둬요. ADR-0016).

## 8. 관련 결정과 요구사항
- ADR-0016(이 문서의 결정), ADR-0006(구조), ADR-0009(KG, 세대), ADR-0010(사전 정의 질의, 엔벨로프), ADR-0012(권한), ADR-0013(관측), ADR-0014(정의), ADR-0015(역량 질문), ADR-0017(운영), ADR-0018(시크릿).
- REQ-143(버전이 있는 계약, 계약이 먼저), REQ-147(API와 CLI), REQ-148(형식 규칙), REQ-149(두 어댑터).
