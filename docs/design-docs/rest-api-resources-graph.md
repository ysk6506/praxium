---
title: REST API 자원과 경로 (2) — KG, 관측, 역량 질문, 권한, 바탕
kind: reference
status: active
follows:
  - docs/design-docs/adr/0016-frontend-separate-rest-api-contract.md
---
# REST API 자원과 경로 (2): KG, 관측, 역량 질문, 권한, 바탕

[REST API 설계](rest-api.md)의 규칙 아래에서 아홉 그룹 가운데 뒤의 다섯의 길을 적어요. 앞의 넷(모델, 수집, 실행, 정의)은 [앞 문서](rest-api-resources-data.md)에 있어요. 읽는 법(역할 필드, 작업 필드, 필터링)은 앞 문서의 1절과 같아요. 시크릿, 운영 액션, 목록, 푸시 수집 같은 더한 기능의 경로는 [더한 기능의 경로](rest-api-resources-additions.md)에 있어요.

> **길과 칸은 다음 변경의 OpenAPI 문서에서 굳어요.** 길은 `/v1/...`이고 행위는 `:행위` 표기 하나예요. **KG에 든 것의 길은 모두 `/v1/kgs/{kg}/...`예요.** `{kg}`는 KG의 ID이고 운영의 KG는 `main`으로도 가리켜요(rest-api.md 3-15). 아래 표에서 `/kgs/{kg}`는 `/v1/kgs/{kg}`의 줄임이에요.

## 1. KG
KG는 변경 시퀀스이고 버전은 확정 버전이에요(ADR-0009). 읽는 경로는 모두 3-2의 엔벨로프(`as_of`(KG의 ID와 세대를 실어요), `freshness`, `evidence`, `result_complete`, `hidden`, `health_hint`)를 달아요. 읽을 수 없는 타입의 객체는 나오지 않고 `hidden`에 있음/없음으로만 잡혀요(범위가 전체인 주체에게는 수도 줘요. ADR-0012). 수를 내는 모든 칸은 가려진 것을 뺀 수예요.

### 1-1. KG 자원, 객체, 링크
| 길 | 메서드 | 하는 일 | 역할 | 작업 | 원천 |
|---|---|---|---|---|---|
| `/v1/kgs`, `/v1/kgs/{kg}` | GET | KG: `kg_id`, `alias`(`main`), `role`(`production`, `candidate`, `test`, `system`), `definition_set`(쓰는 정의 세트), `generation`, `head_change_seq`, `readable_through`, `projection_state`. 후보 KG와 테스트 KG와 시스템 KG도 여기에 있어요 | 읽기(KG를 범위에 가진) | - | kg-storage |
| `/kgs/{kg}/objects` | GET | `find`의 짧은 모양: `filter[object_type][equals]`, `filter[<속성>][equals\|in\|prefix\|range]`, `filter[identifier][equals]`, `filter[text][equals]`, `filter[scope_id][equals]`. 가려진 속성으로 거르면 `permission.hidden_attribute`로 거절해요 | 읽기 | - | kg-queries |
| `/kgs/{kg}/objects/{object_id}` | GET | `get`: `include`로 `attributes`, `identifiers`, `link_counts`, `evidence`, `provenance`. 속성마다 `value`, `unit`, `grade`, `basis`, `seen_at`, `stale` | 읽기 | - | kg-queries |
| `/kgs/{kg}/objects:batch-get` | POST | 여러 객체를 한 번에 읽어요(`object_ids`, 같은 `include`). 안전함 | 읽기 | - | ADR-0016 |
| `/kgs/{kg}/objects/{object_id}/links` | GET | 그 객체의 링크(`link_id`, `link_type`, `peer_object_id`, `grade`). 허브는 `groups`(수)를 먼저 | 읽기 | - | kg-queries |
| `/kgs/{kg}/links/{link_id}` | GET | 링크 하나: 타입, 뜻, 양 끝, 근거 등급, 유효한 버전 구간 | 읽기 | - | kg-queries |
| `/kgs/{kg}/objects/{object_id}/issue-reports` | POST | KG가 틀렸다고 알려요(`user_report`가 문제 목록으로 들어가요. KG를 직접 고치지 않아요) | 읽기를 가진 누구나 | - | kg-operations 2.6 |

공통 입력: `kg_version_id`(없으면 지금), `view`(`as_designed`, `as_observed`), `max_staleness`(소스에서 본 값이 얼마나 낡아도 되나), `min_seq`와 `wait_timeout`(복제가 따라오기를 기다리는 것. 둘은 다른 뜻이에요), `limit`, `cursor`, `shape`. **링크는 `volatile`이 될 수 없어요.** `volatile` 속성은 그 버전 때의 답에서 빠져요(`volatile_as_of: omitted`).

### 1-2. 사전 정의 질의 13종
모두 `POST /v1/kgs/{kg}/queries:<이름>`이에요. 상태를 바꾸지 않아요(계약에 안전함 표시). 큰 입력(객체 목록, 필터링)을 본문으로 받아요. 답은 바로 와요: 시간 예산을 넘기면 202가 아니라 `meta.result_complete.truncated: true`와 `reason: time_budget`이에요(ADR-0010). 길게 걸릴 큰 읽기는 `bulk-reads`(1-4)를 써요. 역할은 `읽기`예요. 입력과 출력의 칸은 [KG에 묻기](kg-queries.md)가 정해요. 아래 표의 길은 `/kgs/{kg}/questions/<이름>`을 `<이름>`으로 줄였어요.

| 질의 | 주 입력 | 답의 `data` | 짝이 되는 GET |
|---|---|---|---|
| `find` | `object_type`(`include_subtypes`), `trait`, `attributes`, `identifier`, `text`, `scope_id` | `counts_by_type`, `items` | `GET /kgs/{kg}/objects` |
| `get` | `object_id`나 `identifier`, `include` | 객체, 속성, 식별자, 유효한 구간 | `GET /kgs/{kg}/objects/{object_id}` |
| `neighbors` | `object_id`, `link_types`, `meanings`, `direction`, `max_steps`, `peer_types` | `groups`, `items` | |
| `containment` | `object_id`, `direction`(`up`, `down`), `depth`, `child_types` | `ancestors`, `children_summary`, `subtree_size`, `items` | |
| `paths` | `from_object_id`, `to_object_id`, `via_meanings`, `link_types`, `max_steps`, `top_k`, `dead`, `dead_links` | `reachable`(`yes`, `no`, `unknown`), `paths`, `found_count` | |
| `impact` | `dead`, `dead_links`, `site_failure`, `dead_scope_id`, `mode`(`both`, `facts_only`, `with_assumptions`), `shape` | 결과별 목록(`down`, `interrupted`, `degraded`, `exposed_direct`, `exposed_inherited`, `unmanageable`, `unknown`), `counts_by_result`, `groups_touched`, `items` | |
| `exposure` | `dead`, `scope_id`, `target_object_id`, `cut_order_max` | `tight_groups`, `cut_sets` | |
| `changes` | `from_version`/`to_version` 또는 `from_change_seq`/`to_change_seq`, `object_types`, `source_id`, `scope_id`, `change_kinds` | 타입별·종류별 수, `items` | `GET /kgs/{kg}/changes` |
| `versions` | `since_version`, `limit` | 확정 버전들 | `GET /kgs/{kg}/graph-versions` |
| `quality` | `scope_id`, `source_id`, `metric`, `kg_version_id` | 지표와 목표 | |
| `health` | `resource`, `scope_id` | 자원의 헬스 상태와 이유 | `GET /kgs/{kg}/health` |
| `scopes` | `scope_id`(없으면 목록) | 저장해 둔 범위와 `count` | `GET /kgs/{kg}/scopes` |
| `competency` | 3절 | 충족률 | |

노출됨은 끝까지 번져요(깊이의 설정이 없어요). `exposed_direct`는 자기의 이중화를 잃은 것이고 `exposed_inherited`는 기대는 것이 노출됨이라서 노출된 것이에요. 답은 둘을 가르고, 무게의 차례는 [KG에 묻기](kg-queries.md)가 정해요.

```json
POST /v1/kgs/main/queries:paths
{"from_object_id": "obj_srv_a", "to_object_id": "obj_sw_1", "via_meanings": ["connected_to"], "max_steps": 6, "top_k": 3, "dead": ["obj_sw_2"]}
→ {"data": {"reachable": "yes", "found_count": 2, "paths": [{"length": 3, "weakest_grade": "assumed", "steps": [...]}]},
   "meta": {"as_of": {"kg": "kg_main_01", "generation": 1, "change_seq": 88231}, "served_by": {"role": "engine"}, "result_complete": {"truncated": false}}}
```

- `impact`, `paths`, `exposure`은 질의 엔진이 답할 수 있어요. 프로젝션이 아직 안 만들어졌으면 `stale.projection_not_ready`(503)예요. `served_by`가 어느 역할이 답했는지 보여 줘요.
- 계산은 전체 그래프에서 하고, 가려진 객체는 결과에 안 나와요. `paths`의 가려진 걸음은 "가려진 객체 하나"로 보여요.

**첫 셋이 선 모양**(ADR-0027, 계약의 `kg` 태그). 길 여덟이 계약에 있다: `GET /v1/kgs`, `/v1/kgs/{kg}`, `/objects`, `/objects/{object_id}`, `/objects/{object_id}/links`와 `POST /v1/kgs/{kg}/queries:find`, `queries:get`, `queries:neighbors`. 짝의 GET은 같은 질의를 부르고 목록 길은 `shape` 기본이 `items`다. POST의 몸은 질의의 칸을 평평하게 받고(`kg_version_id`, `change_seq`, `view`, `min_seq`, `wait_timeout`(ISO 8601 기간), `max_staleness`, `limit`, `cursor`, `shape`), 모르는 칸은 필드별 `422`, JSON이 아닌 몸은 `415`다. 응답 엔벨로프는 `meta`로 간다(`as_of`는 `kg`와 `volatile_as_of`를 품고, `hidden`은 `has_hidden`을 달고, `freshness`, `evidence`, `result_complete`가 함께 간다). 저장소가 없는 서버는 `503 server.unavailable`(`reason: no_store`)로 답한다. 아직 없는 것: `objects:batch-get`, `issue-reports`, 나머지 일곱 질의, `/questions/{question_name}`의 일반 길.

### 1-3. 버전, 변경, 범위, 고정
| 길 | 메서드 | 하는 일 | 역할 | 작업 | 원천 |
|---|---|---|---|---|---|
| `/kgs/{kg}/graph-versions` | GET | 확정 버전의 목록 | 읽기 | - | kg-queries, kg-storage |
| `/kgs/{kg}/graph-versions/{kg_version_id}` | GET | 한 버전: `change_seq`, `created_at`, `definition_version`, `input_digest`, `change_counts`, `projection_state`, `seal_kind`, `executor_version`, `hash_rule_version` | 읽기 | - | kg-storage |
| `/kgs/{kg}/changes` | GET | 변경의 목록(`changes`의 짧은 모양): `from_change_seq`, `to_change_seq`, `filter[object_type]`, `filter[change_kind]`. 페이지네이션의 `cursor`는 세대를 품어요 | 읽기 | - | kg-queries |
| `/kgs/{kg}/scopes`, `/kgs/{kg}/scopes/{scope_id}` | GET, POST, DELETE | 저장해 둔 범위(타입, 포함 뿌리, 필터링)를 읽고 만들고 지워요. 만든 사람의 그룹의 것이고 정의가 아니에요 | 읽기, 만들기와 삭제는 `viewer`부터 | - | kg-queries 10 |
| `/kgs/{kg}/serving-pins` | GET, POST | 묻는 쪽을 옛 KG 버전에 고정(`kg_version_id`, 이유). 엔벨로프의 `as_of.pinned`에 보여요. 상태는 `pending`에서 `active`로 가요 | 전체 범위의 `operator`나 `admin` | - | ADR-0010 |
| `/kgs/{kg}/serving-pins/{pin_id}` | DELETE | 고정 풀기 | 전체 범위의 `operator`나 `admin` | - | ADR-0010 |

### 1-4. 대량 읽기
| 길 | 메서드 | 하는 일 | 역할 | 작업 | 원천 |
|---|---|---|---|---|---|
| `/kgs/{kg}/scopes/{scope_id}/bulk-reads` | POST | 범위 하나를 한 버전에서 통째로 읽어요(`kg_version_id`). 쪽 단위 흐름으로 내려받아요 | `viewer` + `bulk_read` 허가 | 작업 | kg-queries 10 |
| `/v1/jobs/{job_id}/result` | GET | 결과의 흐름(줄마다 JSON, 첫 줄이 엔벨로프). `cursor`로 이어 받아요 | 작업을 만든 사람 | - | rest-api 3-13 |

가림이 적용되고 쓴 사람이 감사 로그(`bulk_read`)에 남아요. 에이전트에게(MCP) 주지 않아요.

### 1-5. 재수집, 견주기, 식별 이벤트, 대기 중인 값, 완전성 선언
| 길 | 메서드 | 하는 일 | 역할 | 작업 | 원천 |
|---|---|---|---|---|---|
| `/kgs/{kg}/refreshes` | POST | **지정 재수집**: `object_ids`를 주면 그 객체에 값을 대는 피드의 소스에서 그 객체의 것만 지금 다시 읽어 같은 길로 적용해요. 소스를 읽기만 해요 | `operator`, `viewer`(그 객체를 읽는 범위. 소스 보호의 한도 안에서) | 작업 | ADR-0008 |
| `/kgs/{kg}/refreshes/{refresh_id}` | GET | 결과: 속성마다 바뀐 것, `source_time`, 적용된 `change_seq`, 어느 소스가 다시 읽혔고 어느 것이 못 읽혔나, "없음을 봄". 작업의 `result.href`가 이 길이에요 | 작업을 읽는 사람과 객체의 읽기 | - | ADR-0008 |
| `/kgs/{kg}/value-checks` | POST | **기대와 대조**: 객체의 속성이 이 값이어야 한다는 기대와 기준 시각을 주면 속성마다 `confirmed`, `not_yet`, `contradicted`, `unverifiable`을 줘요. 근거는 `observed`만 쳐요. 안전함 | 읽기 | - | ADR-0010 |
| `/kgs/{kg}/identity-events`, `/kgs/{kg}/identity-events/{id}` | GET | 식별 이력(합침, 가름, 되살림). 합침 때 옛 링크 ID와 새 링크 ID의 이음이 실려요 | 읽기 | - | kg-model 10 |
| `/kgs/{kg}/identity-events/{id}:revert` | POST | **이 합침을 리버트**: 둘을 가르는 예외를 담은 제안을 만들어요(드라이 런과 승인을 거쳐요) | `editor` | - | ADR-0014 |
| `/kgs/{kg}/pending-values` | GET | 붙을 곳이 없어 기한 안에 기다리는 값(`feed_id`, `natural_key`, `attribute`, `received_at`, `expires_at`). 필터링: `feed_id`, `object_type` | 읽기(피드의 자리) | - | pipeline 3 |
| `/kgs/{kg}/completeness-declarations` | GET | 피드의 완전성 선언과 지금 상태(`declared_scope`, `expected_count`, `received_count`, `state`: `active`, `suspended`, `suspended_reason`). 어긋남이 임계값을 넘으면 저절로 멈춰요. 선언을 고치는 경로는 피드의 정의 항목이에요 | 읽기 | - | pipeline 3 |

재수집은 삭제를 만들지 않아요. 기대와 대조는 기대 조건(`/v1/expectations`, 정의의 항목)과 다른 자원이에요. 영향 범위처럼 이웃에 번지는 것의 확인은 같은 변경 번호를 `min_seq`로 주고 물어요.

## 2. 관측
헬스 상태, 지표, 목표, 알림, 문제 목록, 타임라인이에요(ADR-0011, ADR-0013). 목표, 알림의 규칙, 안전장치의 값은 정의의 항목이라 고치는 경로는 앞 문서 5그룹(정의)의 브랜치 항목이고, 여기서는 읽어요. 헬스 상태 상태는 `healthy`, `degraded`, `failing`, `paused`, `unknown`, `affected_upstream` 여섯이에요. 문제 목록과 헬스 상태는 KG에 든 것이라 `/kgs/{kg}/...`이고, 지표와 목표와 알림은 배포의 자원(소스, 데이터셋, 정의)을 측정해서 `/v1/...`이며 KG를 고르는 칸(`kg`)을 받아요.

| 길 | 메서드 | 하는 일 | 역할 | 작업 | 원천 |
|---|---|---|---|---|---|
| `/kgs/{kg}/health` | GET | 자원의 헬스 상태 목록(`filter[resource_kind]`, `filter[health_state]`, `filter[owner]`, `filter[source_id]`). 낮은 비용이에요. 프락시움 자신의 부품의 헬스 상태는 시스템 KG의 이 길로 읽어요 | 읽기 | - | data-observability 5, 11 |
| `/kgs/{kg}/health/{resource_kind}/{resource_id}` | GET | **자원의 헬스 상태 요약**: `health_state`, `own_state`, `since`, `checks`, `cause`, `affected`, `readers`. `include`로 `checks`, `cause`, `downstream`, `impact`, `timeline`. 안 보이는 자원은 `not_found.resource` | 읽기 | - | data-observability 11 |
| `/v1/metrics` | GET | 지표의 이름 목록(자원 종류, 이름표) | 읽기 | - | data-observability 4 |
| `/v1/metrics/query` | POST | 지표 선: `resource` 또는 `scope`, `kg`, `metrics`, `from`, `to`나 `kg_versions`, `window`, `compare`, `include`. 안전함. "KG 버전마다"는 확정한 때의 값을 따로 남긴 것이에요 | 읽기 | - | data-observability 11 |
| `/v1/objectives`, `/v1/objectives/{objective_id}` | GET | 목표(`target`, `kind`)와 지금 상태(`miss_ratio`, `budget_left`, `state`) | 읽기 | - | data-observability 6 |
| `/v1/alert-rules`, `/v1/alert-rules/{rule_id}` | GET | 알림의 규칙(닫힌 아홉 종류) | 읽기 | - | data-observability 8.1 |
| `/v1/alerts`, `/v1/alerts/{alert_id}` | GET | 알림: `open`, `acknowledged`, `snoozed`, `escalated`, `resolved` | 읽기 | - | data-observability 8.3 |
| `/v1/alerts/{alert_id}:acknowledge`, `:snooze`, `:hand-over` | POST | 확인함 / 스누즈(`until`) / 넘기기(`to`). 자원의 범위를 가진 `operator` | `operator` | - | data-observability 8.3 |
| `/kgs/{kg}/issues`, `/kgs/{kg}/issues/{issue_id}` | GET | **문제 목록**: `kind`(`dangling_link`, `attribute_conflict`, `invalid_value`, `identity_candidate`, `completeness_mismatch`, `unattached_value`, `intent_observation_mismatch`, `expectation_violation`, `retired_type_in_use`, `user_report`, `data_alert`), `status`, `subject_ref`, `rule_ref`, `source_ids`, `cause_key`, `first_seen_version`, `last_seen_version`, `proposal_ref` | 읽기 | - | kg-operations 2 |
| `/kgs/{kg}/issues/{issue_id}:open-proposal` | POST | 이 문제를 풀려고 브랜치와 제안을 열어요(`proposal_ref`가 붙어요) | `editor` | - | kg-operations 2.4 |
| `/v1/resources/{resource_kind}/{resource_id}/timeline` | GET | **타임라인**: `at`, `kg_version`, `event_kind`, `severity`, `ref`, `summary`. `include_lineage=up\|down` | 읽기 | - | data-observability 9.4 |
| `/v1/usage-records` | GET | 서비스 계정마다 어느 타입과 속성을 읽었나(하루 집계) | `admin`, `auditor` | - | access-control 11 |

타임라인의 사건 종류는 실행, 헬스 상태와 알림, 정의와 KG(`definition_merged`, `emergency_revert`, `kg_version_sealed`, `serving_pinned` 등)예요.

```json
GET /v1/kgs/main/health/object_type/Interface?include=cause,readers
→ {"data": {"health_state": "affected_upstream", "own_state": "healthy",
            "cause": [{"resource": {"kind": "source", "id": "switch_inventory"}, "check": "auth_failed", "since": "2031-04-01T08:10:00Z"}]}, "meta": {...}}
```

## 3. 역량 질문
역량 질문은 정의의 항목이에요(ADR-0015). 만들고 고치는 경로는 앞 문서 5그룹(정의)의 브랜치 항목(`kind: competency_question`)이에요. 여기서는 읽고 충족률을 물어요.

| 길 | 메서드 | 하는 일 | 역할 | 작업 | 원천 |
|---|---|---|---|---|---|
| `/v1/competency-questions`, `/v1/competency-questions/{question_id}` | GET | 역량 질문(물음의 글, 담당자, 대상, 필요한 것의 패턴, 목표 충족률, `block_on_regression`, `tracked_groupings`). 필터링: `origin`, `layer`, `object_type` | 읽기 | - | competency-questions |
| `/kgs/{kg}/questions/competency` | POST | **충족률.** 입력: `question_ids`, `origins`, `layers`, `object_types`, `object_ids`, `filter`, `group_by`, `mode`, `breakdown`, `history`, `include`(`rates`, `unmet_objects`, `gaps`, `needs`). 안전함. 분모와 "가려진 객체"는 가려진 것을 뺀 수예요 | 읽기 | - | competency-questions 7 |

- `history`는 질문 전체나 `tracked_groupings`의 묶음만 돼요. 다른 `filter`나 `group_by`와 함께 쓰면 `invalid_input.history_with_filter`로 거절해요. 가려진 속성으로 거르거나 묶으면 거절해요.
- `include: unmet_objects`는 `items`를 페이지네이션으로, `include: gaps`는 필요마다 `blocking_alone`과 갈래를 줘요.

## 4. 권한
사람과 그룹은 사내 SSO가 알고, 프락시움은 역할과 범위를 붙여요(ADR-0012). 역할은 여덟이에요: `viewer`, `editor`, `approver`, `operator`, `model_steward`, `auditor`, `admin`, `ingester`(범위에 든 소스에 밀어 넣고 수집 엔벨로프를 보내요. 그 밖은 아무것도 못 해요).

| 길 | 메서드 | 하는 일 | 역할 | 작업 | 원천 |
|---|---|---|---|---|---|
| `/v1/me` | GET | 나: `subject`, `grants`, `bulk_read`, `allowed_actions`(`[{action, allowed, reason_code}]`), `capabilities` | 로그인한 누구나 | - | access-control |
| `/v1/directory/subjects` | GET | **사람과 그룹 찾기**(SSO). 필터링: `filter[name][prefix]`, `filter[kind][equals]` | `admin`, `editor`(담당자 고르기) | - | access-control 3-2 |
| `/v1/roles` | GET | 역할 여덟과 할 수 있는 일의 표 | 로그인한 누구나 | - | access-control 4 |
| `/v1/grants`, `/v1/grants/{grant_id}` | GET, POST | 권한 부여를 읽고 줘요. 칸: `subject`, `role`, `scope`(`kg`(기본 `main`), `all`, `object_types`, `sources`, `max_sensitivity`, `object_conditions`), `delegation`(범위 한정 위임의 자리), `expires_at`, `bulk_read`. `object_conditions`와 `delegation`은 칸이 알파부터 있고 구현은 클로즈드 베타예요. `Idempotency-Key` 필요 | 읽기 `auditor`, `admin` / 쓰기 `admin` | - | access-control 6 |
| `/v1/grants/{grant_id}:revoke` | POST | 거두기(이력을 남겨요) | `admin` | - | access-control 6 |
| `/v1/owners` | GET | **담당자**(연락처)를 항목과 자원마다 모아서 읽어요. 권한이 아니에요 | 읽기 | - | access-control 8 |
| `/v1/system-accounts`, `/v1/system-accounts/{account_id}` | GET, POST, PATCH | 서비스 계정: `display_name`, `contacts`, `bound_persons`(`on_behalf_of`로 고를 수 있는 사람), `expires_at`, `rotated_at`, `rate_limit`, `status`. 토큰은 만들 때 한 번만 보여요 | `admin` | - | access-control 11 |
| `/v1/system-accounts/{account_id}:rotate-token`, `:disable` | POST | 토큰 바꾸기(짧은 겹침 뒤에 옛 토큰이 막혀요) / 끄기. 저장소가 살아 있으면 30초 안에 퍼져요 | `admin` | - | access-control 11 |
| `/v1/audit-records` | GET | **감사 로그**: `audit_id`, `occurred_at`, `actor_kind`, `actor_id`, `action`, `target_kind`, `target_id`, `grant_ids_used`, `outcome`, `admin_override`, `proposal_id`, `request_id`. 필터링: `actor_id`, `action`, `target_id`, `occurred_at`(`range`). 더하기만 하는 기록 | `auditor` | - | access-control 12 |
| `/v1/blocked-items/{item_id}:resolve` | POST | 막힌 일 풀기. 감사에 `admin_override` | `admin` | - | access-control 10-2 |

- **가리는 규칙.** 읽을 수 없는 것은 목록에서 빠지고 한 건을 읽으면 `not_found.resource`예요. 읽을 수 있는데 못 하는 일은 `permission.*`(403)예요. 읽기의 판단은 부여마다 (타입 고르기, 객체 조건, 민감 등급의 한도)이고 객체 하나의 한도는 그 객체에 맞는 부여들 가운데 가장 높은 것이에요(타입마다가 아니에요).
- 서비스 계정의 토큰 값은 응답에 다시 나오지 않아요. `on_behalf_of`는 관리자가 그 계정에 묶어 둔 사람 가운데서만 골라요.
- 권한 부여와 서비스 계정의 쓰기에는 `Idempotency-Key`가 필요해요.

## 5. 바탕
작업, 사건의 길, 설정, 계약 문서, 서버가 무엇을 할 수 있나예요. 길은 모두 `/v1/...`예요.

| 길 | 메서드 | 하는 일 | 역할 | 작업 | 원천 |
|---|---|---|---|---|---|
| `/v1/jobs`, `/v1/jobs/{job_id}` | GET | 작업 목록(내가 만든 것)과 한 작업(`state`, `progress`, `result`, `error`, `scope_ref`, `expires_at`). 속한 제안을 읽는 사람도 한 작업의 진행을 읽어요 | 만든 사람, 속한 자원을 읽는 사람 | - | rest-api 3-7 |
| `/v1/jobs/{job_id}:cancel` | POST | 취소. 끝난 일은 `conflict.state_not_allowed` | 만든 사람 | - | rest-api 3-7 |
| `/v1/jobs/{job_id}/result` | GET | 결과(1-4) | 결과 자원의 읽기 권한 | - | rest-api 3-13 |
| `/v1/events/stream` | GET | 사건의 흐름(서버 전송 이벤트). `kinds`, `resource`, `kg`로 거르고 `Last-Event-ID`로 이어 받아요. 세대가 다르면 `conflict.generation_changed` | 읽기(사건마다 읽기 권한을 따라요) | - | rest-api 3-10 |
| `/v1/events` | GET | 같은 사건을 번호로: `since=<event_id>`, `kinds`, `resource`, `kg`, `limit`. 너무 옛것이면 `stale.since_too_old` | 읽기 | - | rest-api 3-10 |
| `/v1/settings` | GET, PATCH | 제품의 설정(SSO 연결, 허용할 출처, 한도의 기본값, `solo_approval`). 시크릿은 참조만. `If-Match` 필요 | `admin` | - | access-control, ADR-0017 |
| `/v1/settings/{setting_name}/revisions` | GET, POST | 운영의 설정의 바뀜의 이력과 리버트. 값의 범위를 검사하고 다시 띄워야 먹는 설정은 표시돼요 | `admin` | - | ADR-0017 |
| `/v1/contract`, `/v1/capabilities` | GET | 계약 문서(OpenAPI)와 서버의 버전과 켜진 기능(`api_version`, `features`) | 로그인한 누구나(`/v1/contract`을 로그인 없이 읽을지는 정하지 않았어요) | - | rest-api 5-4 |
| `/v1/problems/{code}` | GET | 오류 `code`의 설명(`type`이 가리키는 길). 목록은 `/v1/catalog/error-codes` | 로그인 없이 | - | rest-api 3-9 |
| `/v1/health/live`, `/v1/health/ready`, `/v1/health/started` | GET | 떠 있나, 받을 수 있나, 다 떴나. 내용이 없는 거친 답이고 배포의 점검이 써요. 데이터 헬스 상태(`/kgs/{kg}/health`)과 다른 길이에요 | 로그인 없이 | - | self-observability 5.1 |

진단, 운영 액션과 그 승인, 올리는 중의 상태는 [더한 기능의 경로](rest-api-resources-additions.md) 4절에 있어요. 사건의 `kind` 목록과 형식은 rest-api.md 3-10이에요.

## 6. 알려진 한계와 물어 둘 것
- 문제 목록의 `open-proposal`처럼 "문제에서 제안을 연다"는 편의 길은 원천 문서의 흐름에서 끌어낸 거예요. 새 방침이 아니에요.
- 지표, 목표, 알림이 KG를 고르는 칸으로만 가는 것(길이 `/v1/...`)과 프락시움 자신의 부품의 헬스 상태를 시스템 KG의 헬스 상태 길로 읽는 것은 설계 담당 확인이 필요해요.
- 알림의 확인과 넘기기의 행위 이름(`acknowledge`, `snooze`, `hand-over`)은 상태 이름을 따라 지었어요.
- 설정에서 어떤 칸이 고칠 수 있는 설정인지는 정하지 않았어요.
- 한 객체를 직접 고치는 경로는 없어요. KG는 정의와 원본 레코드의 결과예요(ADR-0009).

## 7. 관련 결정과 요구사항
- ADR-0016. 원천: ADR-0009(저장, 세대), ADR-0010(질의), ADR-0011(운영), ADR-0012(권한), ADR-0013(관측), ADR-0015(역량 질문), ADR-0017(프락시움 자신의 운영).
- REQ-143, REQ-147, REQ-148, REQ-149.
