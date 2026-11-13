---
title: REST API 자원과 길 (2) — KG, 관측, 역량 질문, 권한, 바탕
kind: reference
status: active
follows:
  - docs/design-docs/adr/0016-frontend-separate-rest-api-contract.md
---
# REST API 자원과 길 (2): KG, 관측, 역량 질문, 권한, 바탕

[REST API 설계](rest-api.md)의 규칙 아래에서 아홉 무리 가운데 뒤의 다섯의 길을 적어요. 앞의 넷(모델, 받기, 실행, 정의)은 [앞 문서](rest-api-resources-data.md)에 있어요. 읽는 법(역할 칸, 작업 칸, 거르기)은 앞 문서의 1절과 같아요.

> **길과 칸은 다음 변경의 OpenAPI 문서에서 굳어요. 그때 바뀔 수 있어요.** 길은 `/v1/...`이고 이름은 이 문서가 정한 가정이에요.

## 1. KG
KG는 변경의 이어짐이고 버전은 봉한 자리예요(ADR-0009). 읽는 길은 모두 3-2의 봉투(`as_of`, `freshness`, `evidence`, `completeness`, `hidden`(`hidden_passed_count`, `hidden_reached_count`), `health_hint`)를 달고, 읽을 수 없는 타입의 객체는 나오지 않고 `hidden.hidden_passed_count`에 수로만 잡혀요(ADR-0012).

### 1-1. 객체와 링크
| 길 | 메서드 | 하는 일 | 역할 | 작업 | 원천 |
|---|---|---|---|---|---|
| `/v1/objects` | GET | `find`의 짧은 모양: `filter[object_type][equals]`, `filter[<속성>][equals\|in\|prefix\|range]`, `filter[identifier][equals]`(종류와 값), `filter[text][equals]`, `filter[scope_id][equals]`. 가려진 속성으로 거르면 `invalid_input.filter_field_unknown`이 아니라 `permission.hidden_attribute`로 거절해요 | 읽기 | - | kg-queries |
| `/v1/objects/{object_id}` | GET | `get`: `include`로 `attributes`, `identifiers`, `link_counts`, `evidence`, `provenance`. 속성마다 `value`, `unit`, `grade`, `basis`, `seen_at`, `stale` | 읽기 | - | kg-queries |
| `/v1/objects/{object_id}/links` | GET | 그 객체의 링크(`link_id`, `link_type`, `peer_object_id`, `grade`). 허브는 `groups`(수)를 먼저 | 읽기 | - | kg-queries |
| `/v1/links/{link_id}` | GET | 링크 하나: 타입, 뜻, 양 끝, 근거 등급, 유효한 버전 구간 | 읽기 | - | kg-queries |
| `/v1/objects/{object_id}/issue-reports` | POST | KG가 틀렸다고 알려요(`user_report`가 문제 목록으로 들어가요. KG를 직접 고치지 않아요) | 읽기를 가진 누구나 | - | kg-operations 2.6 |

공통 입력: `kg_version_id`(없으면 지금), `view`(`as_designed`, `as_observed`), `max_staleness`, `limit`, `cursor`, `shape`.

### 1-2. 닫힌 질의 열셋
모두 `POST /v1/questions/<이름>`이에요. 상태를 바꾸지 않아요(계약에 안전함 표시). 큰 입력(객체 목록, 거름)을 본문으로 받아요. 답은 바로 와요: 시간 예산을 넘기면 202가 아니라 `meta.completeness.truncated: true`와 `reason: time_budget`이에요(ADR-0010). 길게 걸릴 큰 읽기는 `bulk-reads`(1-4)를 써요. 모든 역할이 자기 범위의 읽기를 포함하므로 역할은 `읽기`예요. 입력과 출력의 칸은 [KG에 묻기](kg-queries.md)가 정해요.

| 질의 | 길 | 주 입력 | 답의 `data` | 짝이 되는 GET |
|---|---|---|---|---|
| `find` | `/v1/questions/find` | `object_type`(`include_subtypes`), `trait`, `attributes`(이름, 연산, 값), `identifier`, `text`, `scope_id` | `counts_by_type`, `items` | `GET /v1/objects` |
| `get` | `/v1/questions/get` | `object_id`나 `identifier`, `include` | 객체, 속성, 식별자, 유효한 구간 | `GET /v1/objects/{object_id}` |
| `neighbors` | `/v1/questions/neighbors` | `object_id`, `link_types`, `meanings`, `direction`, `max_steps`, `peer_types` | `groups`, `items` | |
| `containment` | `/v1/questions/containment` | `object_id`, `direction`(`up`, `down`), `depth`, `child_types` | `ancestors`, `children_summary`, `subtree_size`, `items` | |
| `paths` | `/v1/questions/paths` | `from_object_id`, `to_object_id`, `via_meanings`, `link_types`, `max_steps`, `top_k`, `dead`, `dead_links` | `reachable`(`yes`, `no`, `unknown`), `paths`, `found_count` | |
| `impact` | `/v1/questions/impact` | `dead`, `dead_links`, `site_failure`, `dead_scope_id`, `mode`(`both`, `facts_only`, `with_assumptions`), `shape` | 결과별 목록(`down`, `interrupted`, `degraded`, `exposed`, `unmanageable`, `unknown`), `counts_by_result`, `groups_touched`, `items` | |
| `exposure` | `/v1/questions/exposure` | `dead`, `scope_id`, `target_object_id`, `cut_order_max` | `tight_groups`, `cut_sets` | |
| `changes` | `/v1/questions/changes` | `from_version`/`to_version` 또는 `from_change_seq`/`to_change_seq`, `object_types`, `source_id`, `scope_id`, `change_kinds` | 타입별·종류별 수, `items`(`change_kind`, `changed_fields`, `change_seq`) | `GET /v1/changes` |
| `versions` | `/v1/questions/versions` | `since_version`, `limit` | 봉한 버전들(`kg_version_id`, `change_seq`, `definition_version`, `is_head`, `projection_state`) | `GET /v1/graph-versions` |
| `quality` | `/v1/questions/quality` | `scope_id`, `source_id`, `metric`, `kg_version_id` | 지표와 목표 | |
| `health` | `/v1/questions/health` | `resource`, `scope_id` | 자원의 건강과 까닭 | `GET /v1/health` |
| `scopes` | `/v1/questions/scopes` | `scope_id`(없으면 목록) | 저장해 둔 범위와 `count` | `GET /v1/scopes` |
| `competency` | `/v1/questions/competency` | 3절 | 충족률 | |

```json
POST /v1/questions/paths
{"from_object_id": "obj_srv_a", "to_object_id": "obj_sw_1", "via_meanings": ["connected_to"], "max_steps": 6, "top_k": 3, "dead": ["obj_sw_2"]}
→ {"data": {"reachable": "yes", "found_count": 2, "paths": [{"length": 3, "weakest_grade": "assumed", "steps": [...]}]},
   "meta": {"as_of": {"change_seq": 88231}, "served_by": {"role": "engine"}, "completeness": {"truncated": false}}}
```

- `impact`, `paths`, `exposure`은 질의 엔진이 답할 수 있어요. 투영이 아직 안 만들어졌으면 `stale.projection_not_ready`(503)예요. `served_by`가 어느 역할이 답했는지 보여 줘요.
- 계산은 전체 그래프에서 하고, 가려진 객체는 결과에 안 나오고 수로만 잡혀요. `paths`의 가려진 걸음은 "가려진 객체 하나"로 보여요.

### 1-3. 버전, 변경, 범위, 고정
| 길 | 메서드 | 하는 일 | 역할 | 작업 | 원천 |
|---|---|---|---|---|---|
| `/v1/graph-versions` | GET | 봉한 버전의 목록(`versions`) | 읽기 | - | kg-queries, kg-storage |
| `/v1/graph-versions/{kg_version_id}` | GET | 한 버전: `change_seq`, `created_at`, `definition_version`, `raw_record_set_id`, `change_counts`, `projection_state`, `seal_kind` | 읽기 | - | kg-storage |
| `/v1/changes` | GET | 변경의 목록(`changes`의 짧은 모양): `from_change_seq`, `to_change_seq`, `filter[object_type]`, `filter[change_kind]` | 읽기 | - | kg-queries |
| `/v1/scopes`, `/v1/scopes/{scope_id}` | GET, POST, DELETE | 저장해 둔 범위(타입, 포함 뿌리, 거름)를 읽고 만들고 지워요. 만든 사람의 무리의 것이고 정의가 아니에요. KG를 바꾸지 않아요 | 읽기, 만들기와 지우기는 `viewer`부터 | - | kg-queries 10 |
| `/v1/serving-pins` | GET, POST | 묻는 쪽을 옛 KG 버전에 고정(`kg_version_id`, 까닭). 봉투의 `as_of.pinned`에 보여요 | 전체 범위의 `operator`나 `admin` | - | ADR-0010, data-observability |
| `/v1/serving-pins/{pin_id}` | DELETE | 고정 풀기 | 전체 범위의 `operator`나 `admin` | - | ADR-0010 |

### 1-4. 대량 읽기
| 길 | 메서드 | 하는 일 | 역할 | 작업 | 원천 |
|---|---|---|---|---|---|
| `/v1/scopes/{scope_id}/bulk-reads` | POST | 범위 하나를 한 버전에서 통째로 읽어요(`kg_version_id`). 쪽 단위 흐름으로 내려받아요 | `viewer` + `bulk_read` 허가 | 작업 | kg-queries 10 |
| `/v1/jobs/{job_id}/result` | GET | 결과의 흐름(줄마다 JSON, 첫 줄이 봉투). `cursor`로 이어 받아요 | 작업을 만든 사람 | - | rest-api 3-13 |

가림이 적용되고 쓴 사람이 감사 기록(`bulk_read`)에 남아요. 에이전트에게(MCP) 주지 않아요.

## 2. 관측
건강, 지표, 목표, 알림, 문제 목록, 시간줄이에요(ADR-0011, ADR-0013). 목표, 알림의 규칙, 안전장치의 값은 정의의 항목이라 고치는 길은 앞 문서 5무리(정의)의 브랜치 항목이고, 여기서는 읽어요. 건강 상태는 `healthy`, `degraded`, `failing`, `paused`, `unknown`, `affected_upstream` 여섯이에요.

| 길 | 메서드 | 하는 일 | 역할 | 작업 | 원천 |
|---|---|---|---|---|---|
| `/v1/health` | GET | 자원의 건강 목록(`filter[resource_kind]`, `filter[health_state]`, `filter[owner]`, `filter[source_id]`). 낮은 비용이에요 | 읽기 | - | data-observability 5, 11 |
| `/v1/health/{resource_kind}/{resource_id}` | GET | **자원의 건강 요약**: `health_state`, `own_state`, `since`, `checks`, `cause`(뿌리 자원과 그 길), `affected`, `readers`. `include`로 `checks`, `cause`, `downstream`, `impact`, `timeline`. 안 보이는 자원은 `not_found.resource` | 읽기 | - | data-observability 11 |
| `/v1/metrics` | GET | 지표의 이름 목록(자원 종류, 이름표) | 읽기 | - | data-observability 4 |
| `/v1/metrics/query` | POST | 지표 선: `resource` 또는 `scope`, `metrics`, `from`, `to`나 `kg_versions`, `window`, `compare`, `include`(`objectives`, `anomalies`). 안전함. 답은 `{resource, metric, detail_key, step_key, group_key, reading, points: [{at, value, numerator, denominator, kg_version}]}` | 읽기 | - | data-observability 11 |
| `/v1/objectives`, `/v1/objectives/{objective_id}` | GET | 목표(`target`, `kind`)와 지금 상태(`miss_ratio`, `budget_left`, `state`) | 읽기 | - | data-observability 6 |
| `/v1/alert-rules`, `/v1/alert-rules/{rule_id}` | GET | 알림의 규칙(닫힌 아홉 종류) | 읽기 | - | data-observability 8.1 |
| `/v1/alerts`, `/v1/alerts/{alert_id}` | GET | 알림: `open`, `acknowledged`, `snoozed`, `escalated`, `resolved`. 알림은 문제 목록의 항목이에요 | 읽기 | - | data-observability 8.3 |
| `/v1/alerts/{alert_id}/acknowledge`, `.../snooze`, `.../hand-over` | POST | 확인함 / 잠시 끔(`until`) / 넘기기(`to`). 자원의 범위를 가진 `operator` | `operator` | - | data-observability 8.3 |
| `/v1/issues`, `/v1/issues/{issue_id}` | GET | **문제 목록**: `kind`(`dangling_link`, `attribute_conflict`, `invalid_value`, `identity_candidate`, `completeness_mismatch`, `unattached_value`, `intent_observation_mismatch`, `expectation_violation`, `retired_type_in_use`, `user_report`, `data_alert`), `status`(`open`, `in_progress`, `resolved`), `subject_ref`, `rule_ref`, `source_ids`, `cause_key`, `first_seen_version`, `last_seen_version`, `proposal_ref`. 거르기와 `cause_key`로 묶기 | 읽기 | - | kg-operations 2 |
| `/v1/issues/{issue_id}/open-proposal` | POST | 이 문제를 풀려고 브랜치와 제안을 열어요(`proposal_ref`가 붙어요) | `editor` | - | kg-operations 2.4 |
| `/v1/resources/{resource_kind}/{resource_id}/timeline` | GET | **시간줄**(`timeline_event`): `at`, `kg_version`, `event_kind`, `severity`, `ref`, `summary`. `include_lineage=up\|down`로 계보 위아래 자원의 사건을 합쳐요 | 읽기 | - | data-observability 9.4 |
| `/v1/usage-records` | GET | 시스템 계정마다 어느 타입과 속성을 읽었나(`usage_record`, 하루 집계) | `admin`, `auditor` | - | access-control 11 |

시간줄의 사건 종류는 실행(`sync_succeeded`, `sync_failed`, `stream_held`, `guard_blocked` 등), 건강과 알림(`health_changed`, `alert_opened` 등), 정의와 KG(`proposal_opened`, `definition_merged`, `definition_reverted`, `emergency_revert`, `kg_version_sealed`, `serving_pinned`, `serving_unpinned` 등)예요.

```json
GET /v1/health/object_type/Interface?include=cause,readers
→ {"data": {"health_state": "affected_upstream", "own_state": "healthy",
            "cause": [{"resource": {"kind": "source", "id": "switch_inventory"}, "check": "auth_failed", "since": "2031-04-01T08:10:00Z"}],
            "readers": [{"account_id": "acct_9", "last_read_at": "2031-04-02T07:00:00Z"}]}, "meta": {...}}
```

## 3. 역량 질문
역량 질문은 정의의 항목이에요(ADR-0015). 만들고 고치는 길은 앞 문서 5무리(정의)의 브랜치 항목(`kind: competency_question`)이에요. 여기서는 읽고 충족률을 물어요.

| 길 | 메서드 | 하는 일 | 역할 | 작업 | 원천 |
|---|---|---|---|---|---|
| `/v1/competency-questions`, `/v1/competency-questions/{question_id}` | GET | 역량 질문(물음의 글, 담당자, 대상, 필요한 것의 패턴, 목표 충족률, `block_on_regression`, `tracked_groupings`). 거르기: `origin`, `layer`, `object_type` | 읽기 | - | competency-questions |
| `/v1/questions/competency` | POST | **충족률.** 입력: `question_ids`, `origins`, `layers`, `object_types`, `object_ids`(객체 모드), `filter`(대상을 거르는 패턴, 몇 걸음이든), `group_by`(`attribute`, `object_type`, `layer`, `path`), `mode`(`both`, `facts_only`, `with_assumptions`), `breakdown`(`none`, `by_step`, `by_need`, `by_source`, `by_grade`), `history`, `include`(`rates`, `unmet_objects`, `gaps`, `needs`). 안전함 | 읽기 | - | competency-questions 7 |

- **거르기와 묶기.** 거른 값은 물을 때 객체마다의 표시를 세어 답해요. `history`는 질문 전체나 `tracked_groupings`의 묶음만 돼요. 다른 `filter`나 `group_by`와 함께 쓰면 `invalid_input.history_with_filter`로 거절해요(competency-questions 5.3).
- **못 채운 객체.** `include: unmet_objects`는 `items`(`object_id`, `display_name`, `unmet`)를 쪽 넘김으로 줘요.
- **빈 곳의 보고.** `include: gaps`는 필요마다 `blocking_alone`, 갈래(`no_supplier`, `supplier_gaps`, `supplier_unhealthy`, `grade_only`), `no_supplier`이면 채울 프리셋의 목록을 줘요. 올릴 수 있는 상한이에요.
- 가려진 속성으로 거르거나 묶으면 거절해요. 읽을 수 없는 타입의 객체는 "가려진 객체 N개"로 수만 줘요.

```json
POST /v1/questions/competency
{"question_ids": ["server_dual_power_chain"], "group_by": {"path": ["mounted_in", "located_in"], "object_type": "Site"}, "mode": "both", "include": ["rates", "gaps"]}
→ {"data": {"groups": [{"key": "site-b", "denominator": 80, "rate_fact": 0.5, "rate_with_assumptions": 0.65, "not_evaluated": 0}]}, "meta": {...}}
```

## 4. 권한
사람과 무리는 사내 SSO가 알고, 프락시움은 역할과 범위를 붙여요(ADR-0012). 읽기는 권한 부여의 범위(객체 타입, 민감 등급의 한도)예요. 역할은 `viewer`, `editor`, `approver`, `operator`, `model_steward`, `auditor`, `admin` 일곱이에요.

| 길 | 메서드 | 하는 일 | 역할 | 작업 | 원천 |
|---|---|---|---|---|---|
| `/v1/me` | GET | 나: `subject`, `grants`(역할과 범위), `bulk_read`, `allowed_actions`, `capabilities`. 자원의 행위 귀띔은 각 응답의 `meta.allowed_actions` | 로그인한 누구나 | - | access-control |
| `/v1/directory/subjects` | GET | **사람과 무리 찾기**(SSO). 거르기: `filter[name][prefix]`, `filter[kind][equals]`(`person`, `group`). 담당자와 권한 부여를 고를 때 써요 | `admin`, `editor`(담당자 고르기) | - | access-control 3-2 |
| `/v1/roles` | GET | 역할 일곱과 할 수 있는 일의 표 | 로그인한 누구나 | - | access-control 4 |
| `/v1/grants`, `/v1/grants/{grant_id}` | GET, POST | 권한 부여(`subject`, `role`, `scope`, `expires_at`, `bulk_read`)를 읽고 줘요. `Idempotency-Key`가 필요해요 | 읽기 `auditor`, `admin` / 쓰기 `admin` | - | access-control 6 |
| `/v1/grants/{grant_id}/revoke` | POST | 거두기(이력을 남겨요) | `admin` | - | access-control 6 |
| `/v1/owners` | GET | **담당자**(연락처)를 항목과 자원마다 모아서 읽어요. 권한이 아니에요. 고치는 일은 정의의 변경(브랜치의 항목의 `contacts`)이에요 | 읽기 | - | access-control 8 |
| `/v1/system-accounts`, `/v1/system-accounts/{account_id}` | GET, POST, PATCH | 시스템 계정: `display_name`, `contacts`, `expires_at`, `rotated_at`, `rate_limit`, `status`(`active`, `disabled`). 토큰은 만들 때 한 번만 보여요 | `admin` | - | access-control 11 |
| `/v1/system-accounts/{account_id}/rotate-token` | POST | 토큰 바꾸기(옛 토큰은 짧은 겹침 뒤에 막혀요) | `admin` | - | access-control 11 |
| `/v1/system-accounts/{account_id}/disable` | POST | 끄기 | `admin` | - | access-control 11 |
| `/v1/audit-records` | GET | **감사 기록**: `audit_id`, `occurred_at`, `actor_kind`, `actor_id`, `action`, `target_kind`, `target_id`, `grant_ids_used`, `outcome`, `admin_override`, `proposal_id`, `request_id`. 거르기: `actor_id`, `action`, `target_id`, `occurred_at`(`range`). 더하기만 하는 기록 | `auditor` | - | access-control 12 |
| `/v1/blocked-items/{item_id}/resolve` | POST | 막힌 일 풀기(`admin`). 감사에 `admin_override` | `admin` | - | access-control 10-2 |

- **가리는 규칙.** 읽을 수 없는 것은 목록에서 빠지고(수는 `hidden`) 한 건을 읽으면 `not_found.resource`예요. 있는데 못 읽는 것과 없는 것을 가르지 않아요. 읽을 수 있는데 못 하는 일은 `permission.*`(403)예요.
- 시스템 계정의 토큰 값은 응답에 다시 나오지 않아요. 만들 때와 바꿀 때만 한 번 줘요.
- 권한 부여와 시스템 계정의 쓰기에는 `Idempotency-Key`가 필요해요.

## 5. 바탕
작업, 사건의 길, 설정, 계약 문서, 서버가 무엇을 할 수 있나예요.

| 길 | 메서드 | 하는 일 | 역할 | 작업 | 원천 |
|---|---|---|---|---|---|
| `/v1/jobs`, `/v1/jobs/{job_id}` | GET | 작업 목록(내가 만든 것)과 한 작업(`state`, `progress`, `result`, `error`, `expires_at`) | 만든 사람 | - | rest-api 3-7 |
| `/v1/jobs/{job_id}/cancel` | POST | 취소. 끝난 일은 `conflict.state_not_allowed` | 만든 사람 | - | rest-api 3-7 |
| `/v1/jobs/{job_id}/result` | GET | 결과(1-4) | 만든 사람 | - | rest-api 3-13 |
| `/v1/events/stream` | GET | 사건의 흐름(서버 전송 이벤트). `kinds`, `resource`로 거르고 `Last-Event-ID`로 이어 받아요 | 읽기(사건마다 읽기 권한을 따라요) | - | rest-api 3-10 |
| `/v1/events` | GET | 같은 사건을 번호로: `since=<event_id>`, `kinds`, `resource`, `limit`. 너무 옛것이면 `stale.since_too_old` | 읽기 | - | rest-api 3-10 |
| `/v1/settings` | GET, PATCH | 제품의 설정(SSO 연결, 허용할 출처의 목록, 한도의 기본값). 비밀 값은 참조만. `If-Match` 필요 | `admin` | - | access-control |
| `/v1/contract` | GET | 계약 문서(OpenAPI) 자체 | 인증 없이 읽을 수 있는지는 정하지 않았어요(rest-api 7절) | - | rest-api 5-4 |
| `/v1/capabilities` | GET | 서버의 버전과 켜진 기능(`api_version`, `features`) | 로그인한 누구나 | - | rest-api 5-4 |
| `/v1/problems/{code}` | GET | 오류 `code`의 설명(`type`이 가리키는 길). 계약의 오류 코드 목록과 같아요 | 로그인 없이 | - | rest-api 3-9 |
| `/v1/health-check` | GET | 이 서버가 살아 있나(생존 신호). 내용이 없어요 | 로그인 없이 | - | data-observability 13 |

사건의 `kind` 목록과 형식은 rest-api.md 3-10이에요. 작업은 만든 사람과 같은 범위의 `admin`만 읽어요.

## 6. 알고 가는 한계와 물어 둘 것
- 문제 목록의 `open-proposal`처럼 "문제에서 제안을 연다"는 편의 길은 원천 문서의 흐름(문제를 풀려고 연 제안의 `proposal_ref`)에서 끌어낸 거예요. 새 방침이 아니라 그 흐름의 길이에요.
- `/v1/directory/subjects`를 `editor`가 쓰는 범위(담당자 고르기)는 가정이에요.
- 알림의 확인과 넘기기의 길 이름(`acknowledge`, `snooze`, `hand-over`)은 상태 이름을 따라 지었어요.
- 설정에서 어떤 칸이 고칠 수 있는 설정인지는 정하지 않았어요.
- 한 객체를 직접 고치는 길은 없어요. KG는 정의와 원본 기록의 결과예요(ADR-0009).

## 7. 관련 결정과 요구사항
- ADR-0016. 원천: ADR-0009(저장), ADR-0010(질의), ADR-0011(운영), ADR-0012(권한), ADR-0013(관측), ADR-0015(역량 질문).
- REQ-143, REQ-147, REQ-148, REQ-149.
