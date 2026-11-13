---
title: REST API 설계 — 계약이 먼저, 한결같은 모양, 로그인, 계약을 지키는 장치, 명령줄 클라이언트
kind: guide
status: active
follows:
  - docs/design-docs/adr/0016-frontend-separate-rest-api-contract.md
---
# REST API 설계

화면은 다른 저장소에 있어요. 그래서 이 저장소의 백엔드와 화면은 **REST API의 계약(OpenAPI 문서) 하나로만** 만나요. 이 문서는 계약 전체에 걸리는 규칙을 적어요. 자원과 길의 목록은 [모델·받기·실행·정의](rest-api-resources-data.md), [KG·관측·역량 질문·권한·바탕](rest-api-resources-graph.md), [더한 기능의 길](rest-api-resources-additions.md)에, 기능마다 길이 있는지의 표는 [기능마다의 길](rest-api-feature-routes.md)에, 끝까지 따라가는 예시는 [예시 셋](rest-api-walkthroughs.md)에 있어요. 결정은 ADR-0016에 있어요.

> **계약은 이제 `backend/internal/contract/openapi/parts`의 조각으로 있고 합친 `openapi.json`이 서버에 내장돼요(ADR-0024). 길과 칸이 이 문서와 다르면 계약이 맞아요. 이 문서의 길 가운데 알파에 있는 것은 24개이고 나머지는 `planned/routes.json`에 단계와 함께 있어요.** 이 문서는 OpenAPI 문서를 쓸 사람이 규칙과 자원의 목차를 헷갈리지 않게 하려는 설계예요. 길은 `/v1/...`로 적어요.

프런트엔드는 별도 저장소예요. 백엔드는 제품 수준으로 설계하고 구현하고, 화면은 알파에서 기능을 시험할 수 있는 만큼(임시 콘솔)만 만들며 UX와 UI는 클로즈 베타에서 제대로 해요.

## 1. 무엇에 답하나
- 화면 개발자가 이 문서만 보고 "어떤 길을 어떤 모양으로 부르고, 무엇이 돌아오고, 틀리면 무엇을 받나"를 알 수 있어야 해요.
- 백엔드 구현자가 이 문서만 보고 OpenAPI 문서를 쓸 수 있어야 해요. 모든 길이 같은 규칙을 따라야 하니까 규칙이 먼저예요.
- 넘겨받은 것 문서 6절의 쟁점 사에는 직접 답하지 않아요. 화면과 백엔드 사이의 이음매를 정해요.

## 2. 한눈에

```
 화면(다른 저장소)    명령줄 클라이언트      다른 시스템       에이전트
 계약에서 만든 클라이언트   (API를 그대로 부름)   (시스템 계정)     (읽기 전용 MCP)
        │                     │                  │               │
        └──────── REST API: /v1/... (OpenAPI 계약) ──────────────┤ MCP 서버
                              │                                   │
                 ┌────────────┴───── 같은 쓰임(use case) ──────────┘
                 │  권한의 판단 한 곳 · 답의 봉투 · 닫힌 질의 열셋 · 정의의 쓰기
                 └── 코어 ── 저장소(PostgreSQL) · 질의 엔진 · 일꾼 · 수집기
```

| 알아 둘 것 | 한 줄 |
|---|---|
| 계약 | OpenAPI 문서. 이 저장소에 있고 서버는 계약에 맞는지 검사받아요 |
| 버전 | 길의 맨 앞 큰 버전(`/v1`). 같은 큰 버전 안에서는 더하기만 해요 |
| 길의 자리 | KG에 든 것은 `/v1/kgs/{kg}/...`(`main`은 운영 KG). 정의, 권한 부여, 비밀 값, 운영은 `/v1/...` 바로 아래 |
| 모양 | 복수형 자원, JSON, `snake_case`, 쪽 넘김은 `limit`과 `cursor`. 행위는 `POST /v1/.../{id}:행위` |
| 답 | `data`와 `meta`. 봉투는 닫힌 질의의 봉투와 같아요 |
| 쓰기 | 정의는 브랜치 안에서만. `Idempotency-Key`와 `ETag`/`If-Match` |
| 오래 걸리는 일 | 202와 작업 자원 |
| 오류 | 표준 모양의 문제 JSON. 안정된 `code`. 글은 화면이 만들어요 |
| 로그인 | 증표를 확인만 해요. 세션이 없어요 |
| 자원 | 아홉 무리 |

## 3. 모양의 규칙
각 규칙에 규칙, 까닭, 짧은 예시를 적어요. 예시는 지어낸 거예요.

### 3-1. 이름과 값
**규칙.** 길은 복수형 자원 이름이고 여러 낱말은 하이픈으로 이어요(`/v1/object-types`). 칸 이름과 고르기 값은 `snake_case`예요. ID는 뜻을 읽지 않는 글자열이에요. 시각은 UTC의 표준 표기(인터넷 시각 표기, `Z`로 끝남)예요. 길의 하이픈 표기는 계약 품질 검사(5-3)가 걸어요. KG에 든 것(객체, 링크, 질의, 변경, 버전, 충족률, 건강, 다시 받기)은 길이 `/v1/kgs/{kg}/...`이고(3-15), 행위는 자원 뒤에 쌍점으로 적어요(3-8).
**까닭.** 이름이 길마다 다르면 화면이 길마다 예외를 짜요. ID를 읽으면 ID 모양이 계약이 돼 버려요. 예: `{"object_id": "obj_7f3a", "created_at": "2031-04-02T09:30:00Z", "result": "degraded"}`

### 3-2. 답의 봉투
**규칙.** 모든 성공 응답은 `data`와 `meta`를 가져요. `meta`의 칸은 닫힌 질의의 봉투(ADR-0010, [KG에 묻기](kg-queries.md))를 그대로 써요. 목록이 아닌 단순한 자원은 `meta`가 작아도 되지만 칸 이름은 같아요.
**까닭.** KG에서 온 답은 "어느 자리까지 반영했고, 얼마나 묵었고, 근거가 무엇이고, 잘렸나"를 늘 말해야 해요. REST와 MCP가 같아야 하고요(3-12).

| `meta`의 칸 | 뜻 |
|---|---|
| `as_of` | `kg`(KG의 ID), `generation`(세대. 3-15), `change_seq`, `sealed_version`(있으면), 고정돼 있으면 `pinned`(고정한 버전과 까닭), `volatile_as_of`(`current` 또는 `omitted`) |
| `served_by` | 어느 역할이 답했나(`api_sql`, `engine`) |
| `freshness` | 쓰인 소스 중 가장 나쁜 값: `worst_source_id`, `oldest_age_seconds`, `state` |
| `evidence` | `min_grade`, `counts`(`fact`, `assumed`, `unknown`), `kinds`, `observation_age_seconds` |
| `result_complete` | `total`, `returned`, `truncated`, `reason`, `next_cursor` |
| `hidden` | 가려진 객체가 있나. 범위가 전체인 주체에게는 `hidden_passed_count`(지나간 수)와 `hidden_reached_count`(닿은 수)를 주고, 그 밖의 주체에게는 있음/없음만 줘요(설정으로 수를 열 수 있어요). 내용은 없어요 |
| `health_hint`, `model_limits`, `warnings` | 쓰인 것 가운데 건강이 나쁜 것(ADR-0013), 이 답이 모르는 것, 경고 |
| `allowed_actions` | 자원의 응답에서만. 지금 이 주체가 이 자원에 할 수 있는 행위(4절) |
| `trace_id` | 추적 번호(3-14). 헤더에도 실려요 |

```json
{"data": {"items": [{"object_id": "obj_7f3a", "object_type": "Server", "display_name": "srv-0417"}]},
 "meta": {"as_of": {"kg": "kg_main_01", "generation": 1, "change_seq": 88231, "sealed_version": "kv_0192", "pinned": null, "volatile_as_of": "current"},
          "served_by": {"role": "api_sql"}, "freshness": {"worst_source_id": "src_inventory", "oldest_age_seconds": 5400, "state": "fresh"},
          "evidence": {"min_grade": "assumed", "counts": {"fact": 11, "assumed": 1, "unknown": 0}},
          "result_complete": {"total": 12, "returned": 12, "truncated": false, "next_cursor": null},
          "hidden": {"has_hidden": false}, "trace_id": "tr_5c1e"}}
```

KG와 상관없는 자원(예: 작업)의 `meta`에는 `as_of`와 `freshness` 같은 KG의 칸이 없어요. 칸이 없다는 것과 값이 비었다는 것은 달라요. **수를 내는 모든 칸**(`total`, `counts_by_*`, `groups_touched`, `would_die_count`, `subtree_size`, `link_counts`, 충족률의 분모, `kg_changed`의 타입별 수 따위)은 "가려진 것을 뺀 수"예요. 그래서 범위가 좁은 주체가 수의 차이로 남의 객체를 헤아릴 수 없어요(ADR-0012).

### 3-3. 쪽 넘김
**규칙.** 목록은 열쇠 기반이에요. 요청에 `limit`(기본 100, 상한 1천은 기본값(설정))과 `cursor`, 응답의 `meta.result_complete.next_cursor`가 다음 쪽의 열쇠예요. 쪽 번호와 건너뛰기(offset)는 없어요. `cursor`는 뜻을 읽지 않는 글자열이고 `kg`, `generation`, `change_seq`, 정렬 키(객체는 객체 ID이고 엔진의 내부 번호가 아니에요)를 품어서, 쪽 도중에 변경이 들어와도 같은 자리로 이어져요. 세대가 달라진 `cursor`는 `conflict.generation_changed`(409)로 거절해요(3-15). 큰 답은 수와 묶음을 먼저 주고(`shape=summary`가 기본) 낱개는 `shape=items`로 받아요.
**까닭.** 변경이 계속 들어오는 KG에서 offset은 항목을 빠뜨리거나 겹쳐요.
```
GET /v1/kgs/main/issues?limit=2&filter[status][in]=open,in_progress
→ {"data": [{"issue_id": "iss_01"}, {"issue_id": "iss_02"}], "meta": {"result_complete": {"returned": 2, "next_cursor": "c_9aF2"}}}
GET /v1/kgs/main/issues?limit=2&cursor=c_9aF2&filter[status][in]=open,in_progress   (같은 거르기와 정렬을 다시 보내요)
```

### 3-4. 거르기, 정렬, 고를 칸 (닫힌 문법)
**규칙.** 거르기는 `filter[<칸>][<연산>]=<값>`이고, 연산은 `equals`, `in`, `prefix`, `range` 넷뿐이에요(`find`의 연산과 같아요). `in`의 값은 쉼표로 가르고, `range`는 `from..to`(한쪽을 비워도 돼요)예요. 정렬은 `sort=<칸>,-<칸>`(마이너스가 내림차순)이고 마지막에 ID가 붙어 순서가 정해져요. 고를 칸은 `fields=<칸>,<칸>`이에요. **자원마다 거를 수 있는 칸과 정렬할 수 있는 칸의 목록이 계약에 닫혀 있어요.** 목록 밖의 칸, 모르는 연산, 식(`and`/`or`)은 거절해요(`invalid_input.filter_field_unknown`, `invalid_input.filter_op_unknown`). 같은 칸을 둘 걸면 그리고(and)예요. 가려진 속성으로 거르면 거절해요(ADR-0012).
**까닭.** 열린 질의 언어는 비용과 권한을 지킬 수 없어요. 닫힌 문법이면 계약이 모든 경우를 적어요.

```
GET /v1/kgs/main/issues?filter[kind][equals]=duplicate_key&filter[status][in]=open,in_progress&filter[last_seen_version][prefix]=kv_01&sort=-last_seen_version&fields=issue_id,kind,status
```

### 3-5. 닫힌 질의는 POST의 "물음" 자원
**규칙.** 닫힌 질의 열셋은 입력이 커서(객체 목록, 거름, 범위) 주소줄에 못 담아요. 그래서 `POST /v1/kgs/{kg}/questions/<질의 이름>`으로 받아요. 이 POST는 상태를 바꾸지 않아요. 계약에 "안전함(`x-safe: true`)"이라고 표시하고(OpenAPI에는 이 표시가 없어 확장 칸으로 둬요), `Idempotency-Key`를 받지 않아요. 부작용이 없어 다시 보내도 돼요. 간단한 `get`과 `find`는 GET도 줘요. 답은 3-2의 봉투예요.
**까닭.** 주소줄은 길이가 정해져 있고 로그에 남아요. 상태를 바꾸지 않는다는 것을 표시해야 클라이언트와 중간 장치가 다시 보내도 되는 줄 알아요.

```
POST /v1/kgs/main/questions/impact
{"dead": ["obj_sw0417"], "mode": "both", "view": "as_observed", "shape": "summary", "limit": 50}
→ 200 {"data": {"counts_by_result": {"down": 3, "degraded": 9}}, "meta": {...}}

GET /v1/kgs/main/objects/obj_7f3a?include=attributes,link_counts       (`get`)
GET /v1/kgs/main/objects?filter[object_type][equals]=Server&filter[display_name][prefix]=srv-04   (`find`)
```

### 3-6. 쓰기: 브랜치 안에서만, 겹침 방지, 동시 수정 막기
**규칙.**
- 정의를 고치는 모든 일은 브랜치 안에서 해요. 본줄기를 바로 고치는 길은 없어요. 만들기는 POST, 일부 고치기는 PATCH(`application/merge-patch+json`, 보낸 칸만 바꿔요), 지우기는 DELETE예요. 여러 항목을 한 번에 쓰는 길은 `POST /v1/branches/{branch_id}/items:batch`예요(한 `Idempotency-Key`, 항목마다 `If-Match`. 모두 되거나 모두 안 돼요. 가정).
- **겹침 방지.** 만들기와 행위의 POST는 `Idempotency-Key`를 받아요. 클라이언트가 만든 겹치지 않는 글자열이에요. 같은 열쇠와 같은 본문이면 처음 응답을 그대로 다시 줘요(`Idempotent-Replayed: true`). 같은 열쇠에 다른 본문이면 거절해요(`conflict.idempotency_key_reused`). 같은 열쇠의 요청이 아직 도는 중이면 `conflict.request_in_progress`예요. 열쇠는 정해진 기간(가정) 남겨요. 열쇠 없는 만들기는 거절해요(`invalid_input.idempotency_key_required`).
- **동시 수정 막기.** 항목을 읽으면 `ETag`(그 판의 표시, 항목의 판 해시에서 나와요)가 와요. PATCH와 DELETE와 항목을 바꾸는 행위는 `If-Match`를 보내야 해요. 없으면 428(`invalid_input.precondition_required`), 안 맞으면 412(`conflict.version_mismatch`)예요. 412의 본문에 `current_etag`와 바뀐 칸이 있어 화면이 다시 읽고 합쳐 보여요.
**까닭.** 네트워크는 같은 요청을 두 번 보내게 하고, 두 사람은 같은 항목을 동시에 고쳐요. 조용히 덮어쓰면 "한 일은 모두 기록되고 되돌릴 수 있다"(ADR-0014)가 깨져요.

```
PATCH /v1/branches/br_12/items/itm_88        Idempotency-Key: 6e1f...   If-Match: "rev-17"
Content-Type: application/merge-patch+json
{"body": {"expected_interval": "PT6H"}}
→ 200 ETag: "rev-18"  {"data": {...}}
(옛 판으로 다시 보내면) → 412 {"code": "conflict.version_mismatch", "current_etag": "rev-18", ...}
```

판의 표시가 안 맞으면 412, 없으면 428로 정했어요(AIP는 409를 쓰지만 이 설계는 HTTP의 일반 규칙을 따라요).

### 3-7. 오래 걸리는 일: 202와 작업 자원
**규칙.** 후보 실행, 시험 수집, 내보내기, 대량 읽기, 다시 돌리기, 지정해서 다시 받기는 받았다고 답하고(202, `Location: /v1/jobs/{job_id}`) 작업 자원을 줘요. 작업은 상태, 진행, 결과의 자리, 취소를 가져요.

| 칸 (`job`) | 뜻 |
|---|---|
| `job_id`, `kind` | `candidate_run`, `trial_ingest`, `definitions_export`, `bulk_read`, `replay`, `refresh` 등 |
| `scope_ref` | 작업이 속한 자원(`proposal_id`, `kg` 따위). 없을 수 있어요 |
| `state` | `queued` / `running` / `succeeded` / `failed` / `cancel_requested` / `canceled` |
| `progress` | `stage`, `done`, `total`, `unit`. 모르면 `total`이 없어요 |
| `result` | 성공하면 `href`(결과를 받는 길)와 `expires_at` |
| `error` | 실패하면 오류의 표준 모양(3-9) |
| `created_by`, `created_at`, `started_at`, `finished_at` | 누가 언제 |
| `expires_at` | 이때 이 작업과 결과가 사라져요. 보관 기간은 가정이고 7일부터 재서 정해요 |

```
queued ─▶ running ─▶ succeeded
   │         ├─────▶ failed
   └─▶ cancel_requested ─▶ canceled      (이미 끝난 일은 취소가 "이미 끝남"으로 답해요)
```

작업의 상태와 진행은 만든 사람, 같은 범위의 `admin`, 그리고 `scope_ref`가 가리키는 제안을 읽을 수 있는 사람이 읽어요(승인자가 후보 실행의 진행을 봐요). 결과를 받는 길(`result.href`)은 결과 자원의 읽기 권한을 따라요. 취소는 만든 사람만 해요: `POST /v1/jobs/{job_id}:cancel`이에요. 같은 일을 두 번 걸어도(겹침 방지) 같은 작업을 줘요. 결과의 크기가 크면 `result.href`가 쪽 단위 흐름이에요(3-13).

예: `POST /v1/proposals/prop_31/candidate-runs`가 `202`와 `Location: /v1/jobs/job_4d2`를 주고, `GET /v1/jobs/job_4d2`가 `state: running`과 `progress`를 줘요.

### 3-8. 상태를 바꾸는 행위
**규칙.** 상태를 바꾸는 행위는 자원 뒤에 쌍점과 행위의 이름을 붙여 POST해요(`POST /v1/proposals/{proposal_id}:approve`). **표기는 이것 하나예요.** 빗금 뒤의 동사(`/approve`)는 쓰지 않아요. 만들어지는 것이 자원이면 빗금과 복수형 이름이에요(`/reverts`, `/candidate-runs`, `/exports`). 행위는 상태의 흐름이 허락하는 것만 돼요. 허락하지 않으면 `blocked.*`나 `conflict.state_not_allowed`로 거절하고 막는 까닭의 목록을 줘요.
**까닭.** "제안을 승인한다"는 칸 하나를 PATCH하는 일이 아니에요. 승인 규칙, 검사, 감사 기록이 따라요. 이름이 있어야 계약이 그 일의 입력과 오류를 적을 수 있어요.

| 행위 | 길 | 일 |
|---|---|---|
| 검토 올리기 | `POST /v1/proposals/{id}:request-review` | `draft` → `in_review` |
| 승인 / 거부 | `:approve`, `:reject` | 승인 규칙의 범위 안에서. 자기가 고친 것은 못 해요. 사람만 해요 |
| 줄에 세우기 / 닫기 | `:enqueue`, `:close` | `approved` → `queued` / `close_reason`을 받아요 |
| 본줄기에 맞추기 | `POST /v1/branches/{id}:align` | 충돌이 없으면 저절로 돼요 |
| 되돌리기 | `POST /v1/proposals/{id}/reverts`, `POST /v1/definition-sets/{set_no}/restores`, `POST /v1/kgs/{kg}/identity-events/{id}:revert` | 새 브랜치와 제안을 만들어요 |
| 흐름 풀기 / 버리기 | `POST /v1/feed-flows/{id}:release`, `:discard` | `held`인 흐름. `operator` |
| 지금 한 번 | `POST /v1/schedules/{id}:run-now` | 작업을 만들어요 |
| 고정 | `POST /v1/kgs/{kg}/serving-pins`, `DELETE .../{pin_id}` | 묻는 쪽을 옛 KG 버전에 고정(전체 범위의 `operator`나 `admin`) |

저장하지 않고 해 보는 행위(`:validate`, `:preview`, `:suggest`)는 안전해서 `Idempotency-Key`가 없어요. 목록은 [더한 기능의 길](rest-api-resources-additions.md)에 있어요.

### 3-9. 오류
**규칙.** 오류는 표준 모양(인터넷 표준의 문제의 세부, `application/problem+json`)이에요. 칸: `type`(오류 `code`의 설명 길), `title`(참고용 영어 한 줄), `status`, `code`(안정된 이름), `params`(화면이 글을 만들 때 쓰는 값), `trace_id`, `errors`(어느 칸이 틀렸나). **글은 화면이 `code`와 `params`로 만들어요.** `title`은 사람이 로그를 읽을 때만 써요. 권한이 없어 안 보이는 것과 없는 것을 가르지 않아요. 둘 다 404 `not_found.resource`예요.
`code`는 `<무리>.<세부>`예요. 한번 낸 `code`의 뜻은 바꾸지 않고, 새 `code`를 더하는 것은 더하기라 같은 큰 버전 안에서 돼요. 목록은 계약에 닫혀 있어요.

| 무리 | 상태 | 뜻 | 예 |
|---|---|---|---|
| `auth` | 401 | 누구인지 못 믿어요 | `auth.token_missing`, `auth.token_expired`, `auth.token_invalid` |
| `permission` | 403 | 보이는 것이지만 그 일은 못 해요 | `permission.role_missing`, `permission.not_in_scope`, `permission.self_approval` |
| `not_found` | 404 | 없거나 안 보여요 | `not_found.resource` |
| `invalid_input` | 400, 422, 428 | 입력이 틀려요 | `invalid_input.field`, `invalid_input.filter_field_unknown`, `invalid_input.precondition_required` |
| `conflict` | 409, 412 | 지금 상태와 맞지 않아요 | `conflict.version_mismatch`, `conflict.state_not_allowed`, `conflict.request_in_progress`, `conflict.generation_changed` |
| `rate_limited` | 429 | 한도를 넘었어요 | `rate_limited.requests`, `rate_limited.concurrency`, `rate_limited.compute_budget` |
| `blocked` | 409 | 막는 까닭이 있어요(`blockers`에 까닭의 목록) | `blocked.proposal` |
| `stale` | 409, 410, 503 | 바탕이 낡았거나 아직 안 됐어요 | `stale.branch_behind_trunk`, `stale.since_too_old`, `stale.projection_not_ready`, `stale.min_seq_not_reached` |
| `server` | 500, 503 | 서버 쪽 일 | `server.internal`, `server.unavailable` |

**칸마다의 오류.** 입력이 틀리면 `errors`에 칸마다 하나씩 줘요. `pointer`는 JSON 포인터(표준)예요.

예 1. 틀린 입력.
```json
{"type": "/v1/problems/invalid_input.field", "title": "Invalid input", "status": 422, "code": "invalid_input.field", "trace_id": "tr_a1",
 "errors": [{"pointer": "/body/expected_interval", "code": "invalid_input.field_format", "params": {"expected": "iso8601_duration"}},
            {"pointer": "/name", "code": "invalid_input.field_required", "params": {}}]}
```
예 2. 안 보이는 것(없는 것과 같아요).
```json
{"type": "/v1/problems/not_found.resource", "title": "Not found", "status": 404, "code": "not_found.resource", "params": {"resource": "object"}, "trace_id": "tr_a2"}
```
예 3. 막는 까닭.
```json
{"type": "/v1/problems/blocked.proposal", "title": "Proposal is blocked", "status": 409, "code": "blocked.proposal", "trace_id": "tr_a3",
 "blockers": [{"blocker": "approvals_missing", "params": {"item_id": "itm_88", "scope": "source:src_inventory"}},
              {"blocker": "check_failed", "params": {"check_kind": "merged_valid", "item_id": "itm_91"}}]}
```

### 3-10. 이어서 받기: 사건의 길과 `since`
**규칙.** 화면이 진행과 바뀜을 보도록 두 길을 내요. 같은 사건을 다르게 받는 거예요.
- `GET /v1/events/stream`: 서버가 보내는 사건의 흐름(`text/event-stream`, 서버 전송 이벤트). 사건마다 `id`(`event_id`)가 붙고 KG의 사건의 `event_id`는 세대를 품어요. 끊겼다 다시 붙을 때 마지막 `event_id`를 `Last-Event-ID` 헤더로 보내면 그다음부터 받아요.
- `GET /v1/events?since=<event_id>`: 같은 사건을 번호로 가져가는 길이에요(쪽 넘김). 놓친 것을 메워요. 지금 가진 것이 너무 옛것이면 `stale.since_too_old`이고, 시점 복구로 세대가 바뀐 뒤의 `event_id`이면 `conflict.generation_changed`예요. 둘 다 화면은 상태를 다시 읽어야 해요. 사건을 남기는 기간은 가정이에요.

둘 다 `kinds`, `resource`(자원 종류와 ID), `kg`(KG의 사건만)로 걸러요. **읽기 권한이 사건에도 적용돼요.** 사건은 내용이 아니라 알림이에요. 화면이 알림을 받고 자원을 다시 읽어요.

| 사건의 `kind` | 무리 | 담는 것 |
|---|---|---|
| `job_progress`, `job_finished` | 바탕 | `job_id`, `state`, `progress` |
| `proposal_state_changed` | 정의 | `proposal_id`, 앞뒤 상태, 막는 까닭의 수 |
| `definition_merged`, `definition_reverted`, `kg_version_sealed` | 정의, KG | `set_no`나 `sealed_version` |
| `kg_changed` | KG | `kg`, `generation`, `change_seq`의 구간, 타입별 수(가려진 것을 뺀 수). 가벼운 변경(`volatile`)의 줄도 따라가요. 내용은 없어요(내용은 `changes`로 읽어요) |
| `health_changed`, `alert_opened`, `alert_resolved` | 관측 | 자원, 상태 |
| `guard_blocked`, `stream_held` | 실행 | 소스, 데이터셋 |
| `serving_pin_changed` | KG | `pin_id`, 고정됨 또는 풀림 |

```
id: ev_000812
event: proposal_state_changed
data: {"proposal_id": "prop_31", "from": "in_review", "to": "approved", "blocker_count": 0}
```

서버는 주기적으로 빈 줄(연결 유지)을 보내요. 증표는 요청의 머리(`Authorization`)에 싣고 주소에는 싣지 않아요. 표준 `EventSource`는 요청 머리를 못 달아서, 클라이언트는 머리를 달 수 있는 읽기(예: fetch로 흐름을 읽기)를 써요.

### 3-11. 한도와 머리
**규칙.** 부르는 쪽(계정)마다의 한도(초당 요청 수, 동시 요청 수, 계산 예산. ADR-0010)와 남은 양을 모든 응답에 실어요. `RateLimit-Policy`와 `RateLimit` 머리(IETF 초안의 모양)를 써요. 넘으면 429와 `Retry-After`(초)로 답하고 본문에 `rate_limited.*`와 `retry_after_seconds`, 좁히는 법을 줘요. 한도의 숫자는 재서 정해요.
**까닭.** 닫힌 질의에는 비용 등급이 있어요. 한도를 숨기면 화면이 무엇을 줄여야 하는지 몰라요. 이 머리는 아직 초안이라 이름이 바뀔 수 있어요(6절).

### 3-12. REST와 MCP는 같은 쓰임 위의 두 어댑터
권한의 판단과 답의 봉투, 오류의 `code`가 둘이 같아요. 대응표는 5-6이에요.

### 3-13. 큰 읽기와 내보내기
**규칙.** 범위 하나를 한 KG 버전에서 통째로 읽는 `bulk_read`와 정의 묶음의 내보내기는 작업(3-7)이에요. 성공하면 `result.href`에서 쪽 단위로 내려받아요. 줄마다 JSON인 흐름(`application/x-ndjson`)이고 첫 줄이 봉투(`meta`)예요. 중간에 끊겨도 `cursor`로 이어 받아요. 가림이 적용돼요. `bulk_read`는 `viewer`의 따로 된 허가가 있어야 하고 쓴 사람이 감사에 남아요(ADR-0012).

```
POST /v1/kgs/main/scopes/scp_servers/bulk-reads   {"kg_version_id": "kv_0192"}   → 202 job_9a
GET  /v1/jobs/job_9a/result?cursor=...   → {"meta": {...}}\n{"object_id": ...}\n{"object_id": ...}\n
```

### 3-14. 추적 번호
**규칙.** 모든 응답은 `X-Trace-Id` 머리와 `meta.trace_id`(오류는 `trace_id`)를 달아요. 요청에 W3C의 `traceparent`가 있으면 이어 받아요(가정). 작업과 동기화 실행 기록의 `trace_id`와 같은 번호예요(ADR-0013).

### 3-15. KG는 계약의 자원이고 세대가 따라다녀요
**규칙.**
- KG는 자원이에요(`GET /v1/kgs`, `GET /v1/kgs/{kg}`). `{kg}`는 KG의 ID이고 운영의 KG는 정해진 이름 `main`으로도 가리켜요. KG에 든 것을 다루는 모든 길은 `/v1/kgs/{kg}/...` 아래예요: 객체, 링크, 닫힌 질의 열셋, 변경, 버전, 충족률, 건강, 문제 목록, 다시 받기, 견주기, 식별 이벤트, 저장해 둔 범위, 고정. 후보 KG와 시험 KG와 시스템 KG도 같은 길로 물어요.
- 정의(타입, 소스, 데이터셋, 공급, 브랜치, 제안), 권한 부여, 비밀 값, 운영은 배포에 하나라서 `/v1/...` 바로 아래예요. 정의의 본줄기는 배포에 하나이고 KG는 그 본줄기의 어느 묶음을 쓰는지 가리켜요(시스템 KG는 `sys` 이름 공간의 항목만 써요).
- 권한 부여의 범위에 KG를 고르는 칸이 있어요(기본은 `main`).
- **세대**(`generation`)는 시점 복구를 할 때마다 오르는 번호예요. `as_of`, `cursor`, 사건의 `event_id`, `min_seq`에 실려요. 세대가 다르면 `conflict.generation_changed`로 거절하고, 화면은 상태를 다시 읽어요.
- **`min_seq`와 `wait_timeout`.** 묻는 쪽이 `min_seq`(적어도 이 변경 번호까지 본 답)와 `wait_timeout`을 줄 수 있어요. 복제본이 못 미치면 주 저장소로 가거나 `wait_timeout`까지 기다리고, 그래도 못 미치면 `stale.min_seq_not_reached`로 거절해요. **`max_staleness`는 이것과 상관없어요.** `max_staleness`는 소스에서 본 값이 얼마나 낡아도 되는가 하나만 뜻해요.
- 큰 배치는 `change_seq` 여러 개로 쪼개 적용되지만, 읽는 쪽은 배치가 끝날 때만 오르는 읽는 자리(`readable_through`)까지만 봐요. `min_seq`와 고정이 이 자리를 따라요.
- "그 버전 때"의 답에서 `volatile` 값은 주지 않아요(지금 것을 섞지 않아요). 봉투의 `volatile_as_of: omitted`가 그것을 알려요.
**까닭.** `/v1`은 같은 큰 버전 안에서 더하기만 해요. KG를 고르는 자리를 뒤에 길에 넣으면 깨는 변경이라 처음부터 길에 둬요. "KG 나누기"는 쓰기 한계를 푸는 수단이 아니에요. 영향 범위가 KG 하나 안에서만 돌아서, 서로 기대지 않는 자산 묶음만 다른 KG가 돼요(ADR-0009).

## 4. 로그인과 권한

백엔드는 **증표를 확인하는 쪽**이에요. 세션을 쥐지 않아요.

```
화면            SSO(OIDC)                 프락시움 API
 │ 1 인증 요청(코드 흐름 + PKCE의 challenge)  │
 ├──────────────▶│                            │
 │ 2 사용자가 SSO에서 로그인                   │
 │◀──────────────┤ 3 인증 코드                 │
 │ 4 코드 + PKCE의 verifier로 증표 교환        │
 ├──────────────▶│                            │
 │◀──────────────┤ 5 증표(access token)        │
 │ 6 Authorization: Bearer <증표>              │
 ├───────────────────────────────────────────▶│ 7 서명·발행자·대상·만료 확인
 │◀───────────────────────────────────────────┤ 8 권한 판단(한 곳) 후 응답
```

- **증표 확인.** 서명(SSO가 공개한 열쇠), 발행자, 대상(이 API), 만료를 확인해요. 사람과 무리의 정보는 SSO가 알고 프락시움은 거기에 역할과 범위를 붙여요(ADR-0012). 만료는 `auth.token_expired`예요. 증표를 새로 받는 일은 화면과 SSO 사이의 일이에요.
- **시스템 계정.** 프락시움이 낸 토큰을 `Authorization: Bearer`로 보내요. 토큰은 만들 때 한 번만 보이고 해시만 저장돼요(ADR-0012). 받기에 쓰는 계정은 `ingester` 역할이에요(밀어 넣기와 수집 봉투만 해요. 읽기도 없어요).
- **명령줄의 사람 로그인.** 기기 인증 흐름(OAuth의 표준)을 써요. 사람은 다른 기기의 브라우저에서 SSO로 로그인하고 명령줄은 증표를 받아요. 시스템 계정은 위의 토큰을 써요.
- **허용할 출처.** 화면이 다른 출처에서 뜨므로 허용할 출처의 목록을 설정으로 받아요. 기본은 빈 목록(막힘)이고, 목록에 없는 출처에는 교차 출처 허용 머리를 주지 않아요.
- **"나" 길.** `GET /v1/me`가 "나는 누구이고 무엇을 할 수 있나"를 줘요.

```json
{"data": {"subject": {"kind": "person", "subject_id": "sub_21", "display_name": "아무개"},
          "grants": [{"grant_id": "gr_5", "role": "editor", "scope": {"kg": "main", "object_types": ["Server"], "sources": ["src_inventory"]}, "expires_at": null}],
          "bulk_read": false,
          "allowed_actions": [{"action": "propose", "allowed": true, "reason_code": null}, {"action": "grant", "allowed": false, "reason_code": "forbidden.role"}],
          "capabilities": {"api_version": "v1", "features": ["competency", "events_stream"]}}}
```

- **자원마다의 행위 귀띔.** 자원을 읽는 응답의 `meta.allowed_actions`는 `[{action, allowed, reason_code}]`예요. `action`은 행위의 이름(3-8의 `:` 뒤의 이름이나 `edit` 같은 것)이고, 막혔으면 `allowed: false`와 `reason_code`가 와요. `reason_code`는 닫힌 목록이고 `/v1/catalog/reason-codes`가 줘요.

| `reason_code` | 뜻 |
|---|---|
| `forbidden.role` | 필요한 역할이 없어요 |
| `forbidden.scope` | 역할은 있으나 이 자원이 범위 밖이에요 |
| `conflict.state` | 자원의 지금 상태가 그 행위를 허락하지 않아요 |
| `blocked.approval_required` | 승인이 먼저 필요해요 |
| `blocked.limit` | 한도(주체마다의 열린 수나 예산)에 걸렸어요 |
| `unavailable.degraded` | 시스템이 아파서 지금은 안 돼요 |

  화면이 단추를 켜고 끄고 까닭을 보이는 데 써요. **귀띔일 뿐이에요.** 판단은 언제나 서버가 하고, 단추를 켜 놓아도 서버는 요청마다 다시 판단해 `permission.*`로 거절할 수 있어요.
- **역할.** 길마다 필요한 역할은 자원 표에 적어요. 역할 이름은 [권한 설계](access-control.md)의 여덟(`viewer`, `editor`, `approver`, `operator`, `model_steward`, `auditor`, `admin`, `ingester`)이에요.

## 5. 계약을 지키는 장치

### 5-1. OpenAPI가 진실이다
계약은 OpenAPI 문서이고 이 저장소에 있어요(위치는 다음 변경이 정해요). **계약을 먼저 쓰고** 서버가 거기에 맞춰요. 코드에서 계약을 뽑지 않아요. 문서 버전은 OpenAPI 3.1로 시작해요(3.2가 나왔어요. 올릴 때는 6절). 계약을 쓰기 전에 [기능마다의 길](rest-api-feature-routes.md)의 표에 빈 곳이 없어야 하고, 이 표를 계약의 검사로 이어요(5-3).

### 5-2. 서버와 계약이 맞는지, 깨는 변경인지
| 검사 | 보는 것 | 실패하는 때 |
|---|---|---|
| 서버와 계약의 일치 | 계약에 있는 길이 서버에 있고 그 반대도 맞나, 응답이 계약의 모양인가 | 계약에 없는 길이 서버에 있거나 그 반대 |
| 깨는 변경 | 앞의 큰 버전의 계약과 견줘요: 길이나 칸을 지움, 칸의 이름이나 형식을 바꿈, 선택 칸을 필수로, 고르기 값을 지움, 응답의 오류 `code` 의미를 바꿈 | 같은 큰 버전 안에서 깨는 변경 |
| 계약의 품질 | 5-3 | 규칙 위반 |

검사 도구는 다음 변경에서 골라요.
### 5-3. 계약의 품질 검사
- 이름 규칙(길은 복수형과 하이픈, 칸과 고르기 값은 `snake_case`).
- **모든 응답에 예시**가 있어요. 오류 응답도요.
- **모든 오류 `code`의 목록**이 계약에 있고 응답이 그 안의 것만 써요.
- 쪽 넘김의 모양(`limit`, `cursor`, `meta.result_complete`)과 오류의 모양(문제의 세부)이 모든 길에서 같아요.
- 닫힌 질의에 "안전함" 표시가 있고 만들기와 행위의 POST에 `Idempotency-Key`가, 바꾸는 길에 `If-Match`가 있어요.
- 모든 길에 필요한 역할(`x-required-role`), 상태를 바꾸지 않는 길의 표시(`x-safe`)와 "오래 걸리나" 표시(`x-long-running`)가 있어요. 목록의 `meta.result_complete`는 `total`, `returned`, `truncated`, `reason`, `next_cursor`를 모두 줘요. 커서는 모델 해시(세대)를 담아서 틀리면 `invalid_input.cursor_invalid`, 세대가 달라지면 `conflict.generation_changed`예요.
- **문서의 기능마다 `/v1`의 길이 있어요.** [기능마다의 길](rest-api-feature-routes.md)의 표가 그 검사의 바탕이에요. 행위는 쌍점 표기만 써요.
- KG에 든 길은 모두 `/v1/kgs/{kg}/...` 아래에 있어요.

### 5-4. 버전, 물러남, 화면이 계약을 쓰는 법
- 큰 버전은 길의 맨 앞(`/v1`)이에요. 같은 큰 버전 안에서는 더하기만 해요: 새 길, 새 선택 칸, 새 응답 칸, 새 오류 `code`. 화면은 모르는 응답 칸을 무시해야 해요(받는 쪽의 규칙).
- 깨는 변경은 새 큰 버전(`/v2`)으로 내요. 옛 버전은 물러남을 알린 뒤 정해진 기간 함께 내요. 물러남은 계약의 `deprecated` 표시와 응답의 `Deprecation`과 `Sunset` 머리(둘 다 인터넷 표준)로 알려요. 기간은 정하지 않았어요(7절).
- **프런트엔드 저장소는 계약의 한 판(태그나 해시)을 고정해서** 거기서 클라이언트를 만들어 써요. 계약이 바뀌면 프런트엔드 저장소가 판을 올리고 만든 클라이언트의 차이를 봐요. 모르는 큰 버전은 쓰지 않아요.
- `GET /v1/contract`가 계약 문서를, `GET /v1/capabilities`가 서버의 버전과 켜진 기능을 줘요.

### 5-5. 명령줄 클라이언트
- **API를 그대로 불러요.** 서버 안의 코드에 직접 닿지 않아요. 그래서 화면 없이도 모든 기능을 시험하고 운영할 수 있어요. 알파의 기준은 "모든 기능이 API와 명령줄 클라이언트로 된다"(REQ-147)예요.
- **명령의 모양은 자원과 행위를 따라가요**: `<무리> <자원> <행위> [인자]`. 도구의 이름은 `praxium`이고 서버와 같은 바이너리에 있어요. 서버는 `praxium api serve`로 띄우고, 클라이언트의 플래그(`--endpoint`, `--token`, `--config`, `--json`)는 명령 앞에 놓아요. 종료 코드는 문제의 묶음을 따라요(ADR-0024).

```
praxium question impact --kg main --dead obj_sw0417 --view as_observed
praxium proposal approve prop_31
praxium job get job_4d2 --wait            # 끝날 때까지 기다려요
```

- 출력은 기본이 JSON이고 `--output table`도 있어요. 종료 코드는 오류의 무리를 따라가요. 로그인은 사람이면 기기 인증 흐름(4절), 시스템이면 계정의 토큰이에요.

### 5-6. MCP와의 대응
MCP 서버는 같은 쓰임 위의 얇은 층이에요. **KG의 MCP는 읽기만 해요.** KG를 고치는 길이 없고 툴 이름은 닫힌 질의의 이름과 같아요. 툴은 `kg`(기본 `main`)를 받아요. 지정해서 다시 받기와 기대와 견주기도 읽는 면에 내요(ADR-0010). 다시 받기는 작업을 만들지만 현실에 쓰지 않고, 소스를 읽어 보통의 길로 적용하는 읽기이기 때문이에요(KG는 원본 기록의 결과예요). 운영의 MCP 면은 따로이고 행위가 있어요([프락시움 자신의 관측성](self-observability.md)).

| 닫힌 질의 | REST | MCP 툴 | 다른 점 |
|---|---|---|---|
| `find` | `GET /v1/kgs/{kg}/objects`, `POST /v1/kgs/{kg}/questions/find` | `find` | |
| `get` | `GET /v1/kgs/{kg}/objects/{object_id}` | `get` | |
| `neighbors`, `containment`, `paths`, `impact`, `exposure` | `POST /v1/kgs/{kg}/questions/<이름>` | 같은 이름 | |
| `changes`, `versions` | `POST /v1/kgs/{kg}/questions/<이름>` | 같은 이름 | REST는 `GET /v1/kgs/{kg}/changes`도 줘요 |
| `quality`, `health` | `POST /v1/kgs/{kg}/questions/<이름>` | 같은 이름 | |
| `scopes` | `GET /v1/kgs/{kg}/scopes` | `scopes` | 범위를 만드는 길은 REST만 |
| `competency` | `POST /v1/kgs/{kg}/questions/competency` | `competency` | |
| 다시 받기 | `POST /v1/kgs/{kg}/refreshes` | `refresh` | 작업을 줘요 |
| 기대와 견주기 | `POST /v1/kgs/{kg}/value-checks` | `check_values` | |
| `bulk_read` | `POST /v1/kgs/{kg}/scopes/{scope_id}/bulk-reads` | 없음 | 에이전트에게 주지 않아요 |
| 정의의 읽기 | `GET /v1/definitions-overview` 등 | `definitions_overview`, `proposal`, `what_depends` 등 | 쓰기는 REST만 |

- 입력의 `shape`는 REST에서 `summary`/`items`이고 MCP에서는 `detail`로 `brief`/`full`을 골라요(ADR-0010). 오류는 둘 다 같은 `code`를 주고 MCP는 다음에 할 일을 덧붙여요.

## 6. 표준과 견준 것
설계를 표준의 말에 맞추고 비었거나 어긋나는 곳을 적어요. 결정을 바꾸지는 않아요. 확인한 것과 가정을 가려 적어요.

| 주제 | 표준의 사정 | 이 설계 |
|---|---|---|
| 오류의 모양 | 문제의 세부(인터넷 표준)가 옛 표준을 대체했어요 | 따라요(3-9) |
| 물러남의 머리 | `Deprecation`과 `Sunset`은 인터넷 표준이에요 | 따라요(5-4) |
| `Idempotency-Key` | IETF 초안이에요(draft-ietf-httpapi-idempotency-key-header-08, 표준이 아님). 재현의 규칙과 오류 상태가 초안에 있어요 | 초안의 뜻을 따라요. 이름이 바뀔 수 있어요 |
| 한도의 머리 | `RateLimit`와 `RateLimit-Policy`도 IETF 초안이에요(draft-ietf-httpapi-ratelimit-headers-11) | 초안을 따라요. 바뀔 수 있어요 |
| 상태를 바꾸지 않는 큰 요청 | 본문을 가진 안전한 메서드(HTTP QUERY 메서드)가 제안돼 있고 OpenAPI 3.2에 `query` 작업이 있어요. 번호와 상태는 확인하지 못했어요 | 결정은 POST의 물음 자원이에요. `QUERY`가 퍼지면 같은 자원에 `QUERY`를 더하는 것은 더하기예요. 7절 |
| 쪽 넘김과 오래 걸리는 일 | Google의 API 개선 제안(AIP-158은 불투명한 쪽 표, AIP-151은 오래 걸리는 일)과 Zalando 지침(#160은 열쇠 기반 쪽 넘김 권장)이 권해요. Microsoft의 지침은 확인하지 못함 | 따라요 |
| 안 보이는 것 | AIP-193은 권한을 먼저 보고 있든 없든 403을 주라고 해요 | 결정은 안 보이는 것은 404(있음과 없음을 안 가름). 같은 목적이고 상태가 달라요. 7절 |
| 길의 버전 | Zalando 지침(#115)은 길에 버전을 넣지 말라고 해요. Azure는 `api-version` 질의값을 써요 | 결정은 길의 큰 버전이에요 |
| 브라우저 앱의 증표 | 브라우저 앱의 OAuth 모범 사례는 업무용 앱에 BFF를 강하게 권해요(번호는 확인하지 못했어요). 코드 흐름과 PKCE, 기기 인증 흐름은 OAuth의 표준이에요 | 결정은 화면이 증표를 쥐고 BFF를 두지 않아요. 7절 |
| 서버 전송 이벤트 | WHATWG 표준. `EventSource`는 요청 머리를 못 달아요 | 머리를 달 수 있는 읽기를 써요(3-10) |
| OpenAPI | 3.1이 널리 쓰이고 3.2(최신 3.2.1)가 나왔어요 | 3.1로 시작해요(가정). 도구가 3.2를 따라오면 올려요 |

`If-Match` 실패는 HTTP의 일반 규칙대로 412예요(AIP는 409). 결정이에요.

## 7. 알고 가는 한계와 아직 정하지 않은 것
**한계(가정).**
- 쪽 크기(기본 100, 상한 1천은 기본값 표의 값), 겹침 방지 열쇠를 남기는 기간, 작업과 사건을 남기는 기간, 한도의 숫자는 가정이고 재서 정해요. 밀어 넣기의 겹침을 거르는 기간은 계약에 적고 기본이 7일이에요.
- 이 문서의 길과 칸은 OpenAPI 문서에서 굳어요. 그때까지는 길의 이름과 칸의 수가 바뀔 수 있어요.
- 더하기만 하는 규칙은 검사가 지켜요. 검사가 모든 깨는 변경을 잡는다는 보장은 없어요.

**아직 정하지 않은 것.**
- 길에 큰 버전을 두는 것과 별개로 깨지 않는 선택 기능(켜진 기능)을 알리는 방법의 세부(`capabilities`의 칸).
- 물러난 큰 버전을 함께 내는 기간은 설정이고 기본값은 12달이에요([기본값 표](defaults.md)).
- 안 보이는 것과 없는 것을 404로 같게 하는 규칙과 쓰기 권한 부족(`permission.*`)이 존재를 알려 주는 정도. 읽을 수 있는 자원에만 `permission.*`를 주는 것으로 시작해요.
- `QUERY` 메서드와 OpenAPI 3.2로 갈지, 화면의 `BFF`가 필요한지(필요하면 프런트엔드 저장소가 둬요. ADR-0016).

## 8. 관련 결정과 요구사항
- ADR-0016(이 문서의 결정), ADR-0006(구조), ADR-0009(KG, 세대), ADR-0010(닫힌 질의, 봉투), ADR-0012(권한), ADR-0013(관측), ADR-0014(정의), ADR-0015(역량 질문), ADR-0017(운영), ADR-0018(비밀 값).
- REQ-143(버전이 있는 계약, 계약이 먼저), REQ-147(API와 명령줄 클라이언트), REQ-148(모양의 규칙), REQ-149(두 어댑터).
