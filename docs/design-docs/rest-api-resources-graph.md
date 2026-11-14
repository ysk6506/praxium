---
title: REST API 자원과 경로 (2) — KG, 관측, 역량 질문, 권한, 기반
kind: reference
status: active
follows:
  - docs/design-docs/adr/0016-frontend-separate-rest-api-contract.md
---
# REST API 자원과 경로 (2): KG, 관측, 역량 질문, 권한, 기반

[REST API 설계](rest-api.md)의 규칙 아래에서 9개 그룹 가운데 뒤의 5개의 경로를 적어요. 앞의 4개(모델, 수집, 실행, 정의)는 [앞 문서](rest-api-resources-data.md)에 있어요. 읽는 방법(역할 필드, 작업 필드, 필터링)은 앞 문서의 1절과 같아요. 시크릿, 운영 액션, 목록, 푸시 수집 같은 추가 기능의 경로는 [추가 기능의 경로](rest-api-resources-additions.md)에 있어요.

> **경로와 필드는 다음 변경의 OpenAPI 문서에서 확정돼요.** 경로는 `/v1/...`이고 액션은 `:액션` 표기 하나예요. **KG에 든 것의 경로는 모두 `/v1/kgs/{kg}/...`예요.** `{kg}`는 KG의 ID이고 운영 KG는 `main`으로도 지정할 수 있어요(rest-api.md 3-15). 아래 표에서 `/kgs/{kg}`는 `/v1/kgs/{kg}`의 줄임이에요.

## 1. KG
KG는 변경 시퀀스이고 버전은 확정 버전이에요(ADR-0009). 읽는 경로는 모두 3-2의 엔벨로프(`as_of`(KG의 ID와 세대를 담아요), `freshness`, `evidence`, `result_complete`, `hidden`, `health_hint`)를 붙여요. 읽을 수 없는 타입의 객체는 나오지 않고 `hidden`에 있음/없음으로만 집계돼요(범위가 전체인 주체에게는 수도 제공해요. ADR-0012). 수치를 반환하는 모든 필드는 숨김 객체를 뺀 수예요.

### 1-1. KG 자원, 객체, 링크
| 경로 | 메서드 | 설명 | 역할 | 작업 | 원천 |
|---|---|---|---|---|---|
| `/v1/kgs`, `/v1/kgs/{kg}` | GET | KG: `kg_id`, `alias`(`main`), `role`(`production`, `candidate`, `test`, `system`), `definition_set`(쓰는 정의 세트), `generation`, `head_change_seq`, `readable_through`, `projection_state`. 후보 KG와 테스트 KG와 시스템 KG도 여기에 있어요 | 읽기(KG를 범위에 가진) | - | kg-storage |
| `/kgs/{kg}/objects` | GET | `find`의 짧은 형태: `filter[object_type][equals]`, `filter[<속성>][equals\|in\|prefix\|range]`, `filter[identifier][equals]`, `filter[text][equals]`, `filter[scope_id][equals]`. 숨김 속성으로 필터링하면 `permission.hidden_attribute`로 거절해요 | 읽기 | - | kg-queries |
| `/kgs/{kg}/objects/{object_id}` | GET | `get`: `include`로 `attributes`, `identifiers`, `link_counts`, `evidence`, `provenance`. 속성별로 `value`, `unit`, `grade`, `basis`, `seen_at`, `stale` | 읽기 | - | kg-queries |
| `/kgs/{kg}/objects:batch-get` | POST | 여러 객체를 한 번에 읽어요(`object_ids`, 같은 `include`). 안전함 | 읽기 | - | ADR-0016 |
| `/kgs/{kg}/objects/{object_id}/links` | GET | 그 객체의 링크(`link_id`, `link_type`, `peer_object_id`, `grade`). 허브는 `groups`(수)를 먼저 | 읽기 | - | kg-queries |
| `/kgs/{kg}/links/{link_id}` | GET | 링크 하나: 타입, 의미, 양 끝, 근거 등급, 유효한 버전 구간 | 읽기 | - | kg-queries |
| `/kgs/{kg}/objects/{object_id}/issue-reports` | POST | KG가 틀렸다고 신고해요(`user_report`가 문제 목록으로 들어가요. KG를 직접 수정하지 않아요) | 읽기를 가진 누구나 | - | kg-operations 2.6 |

공통 입력: `kg_version_id`(없으면 현재), `view`(`as_designed`, `as_observed`), `max_staleness`(소스에서 관측한 값이 얼마나 오래돼도 되는지), `min_seq`와 `wait_timeout`(복제가 따라오기를 기다리는 것. 둘은 다른 의미예요), `limit`, `cursor`, `shape`. **링크는 `volatile`이 될 수 없어요.** `volatile` 속성은 "그 버전 시점"의 응답에서 빠져요(`volatile_as_of: omitted`).

### 1-2. 사전 정의 질의 13종
모두 `POST /v1/kgs/{kg}/queries:<이름>`이에요. 상태를 변경하지 않아요(계약에 안전함 표시). 큰 입력(객체 목록, 필터링)을 본문으로 받아요. 응답은 바로 와요: 시간 예산을 넘기면 202가 아니라 `meta.result_complete.truncated: true`와 `reason: time_budget`이에요(ADR-0010). 오래 걸릴 큰 읽기는 `bulk-reads`(1-4)를 써요. 역할은 `읽기`예요. 입력과 출력의 필드는 [KG 질의](kg-queries.md)가 정해요. 아래 표의 경로는 `/kgs/{kg}/questions/<이름>`을 `<이름>`으로 줄였어요.

| 질의 | 주 입력 | 응답의 `data` | 대응하는 GET |
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

노출됨은 끝까지 전파돼요(깊이 설정이 없어요). `exposed_direct`는 자기 이중화를 잃은 것이고 `exposed_inherited`는 의존하는 대상이 노출됨이라서 노출된 것이에요. 응답은 둘을 구분하고, 심각도 순서는 [KG 질의](kg-queries.md)가 정해요.

```json
POST /v1/kgs/main/queries:paths
{"from_object_id": "obj_srv_a", "to_object_id": "obj_sw_1", "via_meanings": ["connected_to"], "max_steps": 6, "top_k": 3, "dead": ["obj_sw_2"]}
→ {"data": {"reachable": "yes", "found_count": 2, "paths": [{"length": 3, "weakest_grade": "assumed", "steps": [...]}]},
   "meta": {"as_of": {"kg": "kg_main_01", "generation": 1, "change_seq": 88231}, "served_by": {"role": "engine"}, "result_complete": {"truncated": false}}}
```

- `impact`, `paths`, `exposure`는 질의 엔진이 응답해요. 프로젝션이 아직 빌드되지 않았으면 `stale.projection_not_ready`(503)예요. `served_by`가 어느 역할이 응답했는지 보여 줘요.
- 계산은 전체 그래프에서 하고, 숨김 객체는 결과에 나오지 않아요. `paths`의 숨김 홉은 "숨김 객체 하나"로 보여요.

**첫 3종의 구현 상태**(ADR-0027, 계약의 `kg` 태그). 경로 8개가 계약에 있어요: `GET /v1/kgs`, `/v1/kgs/{kg}`, `/objects`, `/objects/{object_id}`, `/objects/{object_id}/links`와 `POST /v1/kgs/{kg}/queries:find`, `queries:get`, `queries:neighbors`. 대응하는 GET은 같은 질의를 호출하고 목록 경로는 `shape` 기본이 `items`예요. POST 본문은 질의 필드를 평탄하게 받고(`kg_version_id`, `change_seq`, `view`, `min_seq`, `wait_timeout`(ISO 8601 기간), `max_staleness`, `limit`, `cursor`, `shape`), 알 수 없는 필드는 필드별 `422`, JSON이 아닌 본문은 `415`예요. 응답 엔벨로프는 `meta`에 담겨요(`as_of`는 `kg`와 `volatile_as_of`를 포함하고, `hidden`은 `has_hidden`이 붙고, `freshness`, `evidence`, `result_complete`가 함께 가요). 스토리지가 없는 서버는 `503 server.unavailable`(`reason: no_store`)로 응답해요. 아직 없는 것: `objects:batch-get`, `issue-reports`, 나머지 7종 질의, `/questions/{question_name}`의 일반 경로.

### 1-3. 버전, 변경, 범위, 고정
| 경로 | 메서드 | 설명 | 역할 | 작업 | 원천 |
|---|---|---|---|---|---|
| `/kgs/{kg}/graph-versions` | GET | 확정 버전의 목록 | 읽기 | - | kg-queries, kg-storage |
| `/kgs/{kg}/graph-versions/{kg_version_id}` | GET | 한 버전: `change_seq`, `created_at`, `definition_version`, `input_digest`, `change_counts`, `projection_state`, `seal_kind`, `executor_version`, `hash_rule_version` | 읽기 | - | kg-storage |
| `/kgs/{kg}/changes` | GET | 변경 목록(`changes`의 짧은 형태): `from_change_seq`, `to_change_seq`, `filter[object_type]`, `filter[change_kind]`. 페이지네이션의 `cursor`는 세대를 포함해요 | 읽기 | - | kg-queries |
| `/kgs/{kg}/scopes`, `/kgs/{kg}/scopes/{scope_id}` | GET, POST, DELETE | 저장해 둔 범위(타입, 포함 루트, 필터링)를 조회하고 생성하고 삭제해요. 만든 사람의 그룹에 속하고 정의가 아니에요 | 읽기, 생성과 삭제는 `viewer`부터 | - | kg-queries 10 |
| `/kgs/{kg}/serving-pins` | GET, POST | 조회 측을 과거 KG 버전에 고정(`kg_version_id`, 이유). 엔벨로프의 `as_of.pinned`에 표시돼요. 상태는 `pending`에서 `active`로 전이돼요 | 전체 범위의 `operator`나 `admin` | - | ADR-0010 |
| `/kgs/{kg}/serving-pins/{pin_id}` | DELETE | 고정 해제 | 전체 범위의 `operator`나 `admin` | - | ADR-0010 |

### 1-4. 대량 읽기
| 경로 | 메서드 | 설명 | 역할 | 작업 | 원천 |
|---|---|---|---|---|---|
| `/kgs/{kg}/scopes/{scope_id}/bulk-reads` | POST | 범위 하나를 한 버전에서 통째로 읽어요(`kg_version_id`). 페이지 단위 스트림으로 내려받아요 | `viewer` + `bulk_read` 허가 | 작업 | kg-queries 10 |
| `/v1/jobs/{job_id}/result` | GET | 결과 스트림(행마다 JSON, 첫 행이 엔벨로프). `cursor`로 이어받아요 | 작업을 생성한 사람 | - | rest-api 3-13 |

숨김 처리가 적용되고 사용한 주체가 감사 로그(`bulk_read`)에 남아요. 에이전트(MCP)에게는 제공하지 않아요.

### 1-5. 재수집, 기대와 대조, 식별 이벤트, 대기 중인 값, 완전성 선언
| 경로 | 메서드 | 설명 | 역할 | 작업 | 원천 |
|---|---|---|---|---|---|
| `/kgs/{kg}/refreshes` | POST | **지정 재수집**: `object_ids`를 주면 그 객체에 값을 제공하는 피드의 소스에서 그 객체의 것만 즉시 다시 읽어 같은 경로로 적용해요. 소스를 읽기만 해요 | `operator`, `viewer`(그 객체를 읽는 범위. 소스 보호의 한도 안에서) | 작업 | ADR-0008 |
| `/kgs/{kg}/refreshes/{refresh_id}` | GET | 결과: 속성별 변경 내용, `source_time`, 적용된 `change_seq`, 어느 소스를 다시 읽었고 어느 소스는 읽지 못했나, "없음을 관측". 작업의 `result.href`가 이 경로예요 | 작업을 읽는 사람과 객체의 읽기 | - | ADR-0008 |
| `/kgs/{kg}/value-checks` | POST | **기대와 대조**: 객체의 속성이 이 값이어야 한다는 기대와 기준 시각을 주면 속성별로 `confirmed`, `not_yet`, `contradicted`, `unverifiable`을 반환해요. 근거는 `observed`만 인정해요. 안전함 | 읽기 | - | ADR-0010 |
| `/kgs/{kg}/identity-events`, `/kgs/{kg}/identity-events/{id}` | GET | 식별 이력(병합, 분리, 복원). 병합할 때 이전 링크 ID와 새 링크 ID의 대응이 포함돼요 | 읽기 | - | kg-model 10 |
| `/kgs/{kg}/identity-events/{id}:revert` | POST | **이 병합을 리버트**: 둘을 분리하는 예외를 담은 제안을 생성해요(드라이 런과 승인을 거쳐요) | `editor` | - | ADR-0014 |
| `/kgs/{kg}/pending-values` | GET | 연결할 대상이 없어 기한 안에 대기 중인 값(`feed_id`, `natural_key`, `attribute`, `received_at`, `expires_at`). 필터링: `feed_id`, `object_type` | 읽기(피드의 소속) | - | pipeline 3 |
| `/kgs/{kg}/completeness-declarations` | GET | 피드의 완전성 선언과 현재 상태(`declared_scope`, `expected_count`, `received_count`, `state`: `active`, `suspended`, `suspended_reason`). 불일치가 임계값을 넘으면 자동으로 중단돼요. 선언을 수정하는 경로는 피드의 정의 항목이에요 | 읽기 | - | pipeline 3 |

재수집은 삭제를 생성하지 않아요. 기대와 대조는 기대 조건(`/v1/expectations`, 정의의 항목)과 다른 자원이에요. 영향 범위처럼 이웃으로 전파되는 것의 확인은 같은 변경 번호를 `min_seq`로 지정해 조회해요.

## 2. 관측
헬스 상태, 지표, 목표, 알림, 문제 목록, 타임라인이에요(ADR-0011, ADR-0013). 목표, 알림 규칙, 안전장치의 값은 정의의 항목이라 수정 경로는 앞 문서 5그룹(정의)의 브랜치 항목이고, 여기서는 읽어요. 헬스 상태 값은 `healthy`, `degraded`, `failing`, `paused`, `unknown`, `affected_upstream` 6가지예요. 문제 목록과 헬스 상태는 KG에 든 것이라 `/kgs/{kg}/...`이고, 지표와 목표와 알림은 배포의 자원(소스, 데이터셋, 정의)을 측정하므로 `/v1/...`이며 KG를 선택하는 필드(`kg`)를 받아요.

| 경로 | 메서드 | 설명 | 역할 | 작업 | 원천 |
|---|---|---|---|---|---|
| `/kgs/{kg}/health` | GET | 자원의 헬스 상태 목록(`filter[resource_kind]`, `filter[health_state]`, `filter[owner]`, `filter[source_id]`). 낮은 비용이에요. 프락시움 자체 구성 요소의 헬스 상태는 시스템 KG의 이 경로로 읽어요 | 읽기 | - | data-observability 5, 11 |
| `/kgs/{kg}/health/{resource_kind}/{resource_id}` | GET | **자원의 헬스 상태 요약**: `health_state`, `own_state`, `since`, `checks`, `cause`, `affected`, `readers`. `include`로 `checks`, `cause`, `downstream`, `impact`, `timeline`. 보이지 않는 자원은 `not_found.resource` | 읽기 | - | data-observability 11 |
| `/v1/metrics` | GET | 지표의 이름 목록(자원 종류, 레이블) | 읽기 | - | data-observability 4 |
| `/v1/metrics/query` | POST | 지표 시계열: `resource` 또는 `scope`, `kg`, `metrics`, `from`, `to`나 `kg_versions`, `window`, `compare`, `include`. 안전함. "KG 버전별"은 확정한 시점의 값을 따로 남긴 것이에요 | 읽기 | - | data-observability 11 |
| `/v1/objectives`, `/v1/objectives/{objective_id}` | GET | 목표(`target`, `kind`)와 현재 상태(`miss_ratio`, `budget_left`, `state`) | 읽기 | - | data-observability 6 |
| `/v1/alert-rules`, `/v1/alert-rules/{rule_id}` | GET | 알림 규칙(고정된 9종) | 읽기 | - | data-observability 8.1 |
| `/v1/alerts`, `/v1/alerts/{alert_id}` | GET | 알림: `open`, `acknowledged`, `snoozed`, `escalated`, `resolved` | 읽기 | - | data-observability 8.3 |
| `/v1/alerts/{alert_id}:acknowledge`, `:snooze`, `:hand-over` | POST | 확인 / 스누즈(`until`) / 에스컬레이션(`to`). 자원의 범위를 가진 `operator` | `operator` | - | data-observability 8.3 |
| `/kgs/{kg}/issues`, `/kgs/{kg}/issues/{issue_id}` | GET | **문제 목록**: `kind`(`dangling_link`, `attribute_conflict`, `invalid_value`, `identity_candidate`, `completeness_mismatch`, `unattached_value`, `intent_observation_mismatch`, `expectation_violation`, `retired_type_in_use`, `user_report`, `data_alert`), `status`, `subject_ref`, `rule_ref`, `source_ids`, `cause_key`, `first_seen_version`, `last_seen_version`, `proposal_ref` | 읽기 | - | kg-operations 2 |
| `/kgs/{kg}/issues/{issue_id}:open-proposal` | POST | 이 문제를 해결하려고 브랜치와 제안을 열어요(`proposal_ref`가 붙어요) | `editor` | - | kg-operations 2.4 |
| `/v1/resources/{resource_kind}/{resource_id}/timeline` | GET | **타임라인**: `at`, `kg_version`, `event_kind`, `severity`, `ref`, `summary`. `include_lineage=up\|down` | 읽기 | - | data-observability 9.4 |
| `/v1/usage-records` | GET | 서비스 계정별로 어느 타입과 속성을 읽었나(하루 집계) | `admin`, `auditor` | - | access-control 11 |

타임라인의 이벤트 종류는 실행, 헬스 상태와 알림, 정의와 KG(`definition_merged`, `emergency_revert`, `kg_version_sealed`, `serving_pinned` 등)예요.

```json
GET /v1/kgs/main/health/object_type/Interface?include=cause,readers
→ {"data": {"health_state": "affected_upstream", "own_state": "healthy",
            "cause": [{"resource": {"kind": "source", "id": "switch_inventory"}, "check": "auth_failed", "since": "2031-04-01T08:10:00Z"}]}, "meta": {...}}
```

## 3. 역량 질문
역량 질문은 정의의 항목이에요(ADR-0015). 생성과 수정 경로는 앞 문서 5그룹(정의)의 브랜치 항목(`kind: competency_question`)이에요. 여기서는 읽고 충족률을 조회해요.

| 경로 | 메서드 | 설명 | 역할 | 작업 | 원천 |
|---|---|---|---|---|---|
| `/v1/competency-questions`, `/v1/competency-questions/{question_id}` | GET | 역량 질문(질문 텍스트, 담당자, 대상, 필요한 것의 패턴, 목표 충족률, `block_on_regression`, `tracked_groupings`). 필터링: `origin`, `layer`, `object_type` | 읽기 | - | competency-questions |
| `/kgs/{kg}/questions/competency` | POST | **충족률.** 입력: `question_ids`, `origins`, `layers`, `object_types`, `object_ids`, `filter`, `group_by`, `mode`, `breakdown`, `history`, `include`(`rates`, `unmet_objects`, `gaps`, `needs`). 안전함. 분모는 숨김 객체를 뺀 수예요 | 읽기 | - | competency-questions 7 |

- `history`는 질문 전체나 `tracked_groupings`의 그룹만 가능해요. 다른 `filter`나 `group_by`와 함께 쓰면 `invalid_input.history_with_filter`로 거절해요. 숨김 속성으로 필터링하거나 그룹화하면 거절해요.
- `include: unmet_objects`는 `items`를 페이지네이션으로 반환하고, `include: gaps`는 필요별로 `blocking_alone`과 분류를 반환해요.

## 4. 권한
사람과 그룹은 사내 SSO가 알고, 프락시움은 역할과 범위를 붙여요(ADR-0012). 역할은 8종이에요: `viewer`, `editor`, `approver`, `operator`, `model_steward`, `auditor`, `admin`, `ingester`(범위에 속한 소스에 푸시하고 수집 엔벨로프를 전송해요. 그 밖은 아무것도 못 해요).

| 경로 | 메서드 | 설명 | 역할 | 작업 | 원천 |
|---|---|---|---|---|---|
| `/v1/me` | GET | 현재 사용자: `subject`, `grants`, `bulk_read`, `allowed_actions`(`[{action, allowed, reason_code}]`), `capabilities` | 로그인한 누구나 | - | access-control |
| `/v1/directory/subjects` | GET | **사용자와 그룹 검색**(SSO). 필터링: `filter[name][prefix]`, `filter[kind][equals]` | `admin`, `editor`(담당자 선택) | - | access-control 3-2 |
| `/v1/roles` | GET | 역할 8종과 할 수 있는 작업 표 | 로그인한 누구나 | - | access-control 4 |
| `/v1/grants`, `/v1/grants/{grant_id}` | GET, POST | 권한 부여를 조회하고 부여해요. 필드: `subject`, `role`, `scope`(`kg`(기본 `main`), `all`, `object_types`, `sources`, `max_sensitivity`, `object_conditions`), `delegation`(범위 한정 위임용 필드), `expires_at`, `bulk_read`. `object_conditions`와 `delegation`은 필드가 알파부터 있고 구현은 클로즈드 베타예요. `Idempotency-Key` 필요 | 읽기 `auditor`, `admin` / 쓰기 `admin` | - | access-control 6 |
| `/v1/grants/{grant_id}:revoke` | POST | 회수(이력을 남겨요) | `admin` | - | access-control 6 |
| `/v1/owners` | GET | **담당자**(연락처)를 항목과 자원별로 모아서 조회해요. 권한이 아니에요 | 읽기 | - | access-control 8 |
| `/v1/system-accounts`, `/v1/system-accounts/{account_id}` | GET, POST, PATCH | 서비스 계정: `display_name`, `contacts`, `bound_persons`(`on_behalf_of`로 선택할 수 있는 사람), `expires_at`, `rotated_at`, `rate_limit`, `status`. 토큰은 생성할 때 한 번만 보여요 | `admin` | - | access-control 11 |
| `/v1/system-accounts/{account_id}:rotate-token`, `:disable` | POST | 토큰 교체(짧은 중첩 기간 뒤에 이전 토큰이 차단돼요) / 비활성화. 스토리지가 정상이면 30초 안에 전파돼요 | `admin` | - | access-control 11 |
| `/v1/audit-records` | GET | **감사 로그**: `audit_id`, `occurred_at`, `actor_kind`, `actor_id`, `action`, `target_kind`, `target_id`, `grant_ids_used`, `outcome`, `admin_override`, `proposal_id`, `request_id`. 필터링: `actor_id`, `action`, `target_id`, `occurred_at`(`range`). 추가 전용(append-only) 기록 | `auditor` | - | access-control 12 |
| `/v1/blocked-items/{item_id}:resolve` | POST | 차단된 항목 해제. 감사 로그에 `admin_override` | `admin` | - | access-control 10-2 |

- **숨김 규칙.** 읽을 수 없는 것은 목록에서 빠지고 한 건을 읽으면 `not_found.resource`예요. 읽을 수 있는데 못 하는 작업은 `permission.*`(403)예요. 읽기 판단은 부여별로 (타입 선택, 객체 조건, 민감 등급의 한도)이고 객체 하나의 한도는 그 객체에 해당하는 부여 중 가장 높은 것이에요(타입별이 아니에요).
- 서비스 계정의 토큰 값은 응답에 다시 나오지 않아요. `on_behalf_of`는 관리자가 그 계정에 묶어 둔 사람 가운데서만 선택해요.
- 권한 부여와 서비스 계정의 쓰기에는 `Idempotency-Key`가 필요해요.

## 5. 기반
작업, 이벤트 경로, 설정, 계약 문서, 서버의 지원 기능이에요. 경로는 모두 `/v1/...`이에요.

| 경로 | 메서드 | 설명 | 역할 | 작업 | 원천 |
|---|---|---|---|---|---|
| `/v1/jobs`, `/v1/jobs/{job_id}` | GET | 작업 목록(내가 만든 것)과 한 작업(`state`, `progress`, `result`, `error`, `scope_ref`, `expires_at`). 속한 제안을 읽는 사람도 한 작업의 진행을 읽어요 | 생성한 사람, 속한 자원을 읽는 사람 | - | rest-api 3-7 |
| `/v1/jobs/{job_id}:cancel` | POST | 취소. 끝난 작업은 `conflict.state_not_allowed` | 생성한 사람 | - | rest-api 3-7 |
| `/v1/jobs/{job_id}/result` | GET | 결과(1-4) | 결과 자원의 읽기 권한 | - | rest-api 3-13 |
| `/v1/events/stream` | GET | 이벤트 스트림(SSE). `kinds`, `resource`, `kg`로 필터링하고 `Last-Event-ID`로 이어받아요. 세대가 다르면 `conflict.generation_changed` | 읽기(이벤트별 읽기 권한을 따라요) | - | rest-api 3-10 |
| `/v1/events` | GET | 같은 이벤트를 번호로 조회: `since=<event_id>`, `kinds`, `resource`, `kg`, `limit`. 너무 오래됐으면 `stale.since_too_old` | 읽기 | - | rest-api 3-10 |
| `/v1/settings` | GET, PATCH | 제품의 설정(SSO 연결, 허용할 출처, 한도의 기본값, `solo_approval`). 시크릿은 참조만. `If-Match` 필요 | `admin` | - | access-control, ADR-0017 |
| `/v1/settings/{setting_name}/revisions` | GET, POST | 운영 설정의 변경 이력과 리버트. 값의 범위를 검증하고 재기동해야 적용되는 설정은 표시돼요 | `admin` | - | ADR-0017 |
| `/v1/contract`, `/v1/capabilities` | GET | 계약 문서(OpenAPI)와 서버의 버전과 활성화된 기능(`api_version`, `features`) | 로그인한 누구나(`/v1/contract`을 로그인 없이 읽을지는 정하지 않았어요) | - | rest-api 5-4 |
| `/v1/problems/{code}` | GET | 오류 `code`의 설명(`type`이 가리키는 경로). 목록은 `/v1/catalog/error-codes` | 로그인 없이 | - | rest-api 3-9 |
| `/v1/health/live`, `/v1/health/ready`, `/v1/health/started` | GET | 기동 중인가, 요청을 받을 수 있나, 기동이 끝났나. 내용이 없는 단순 응답이고 배포 점검이 사용해요. 데이터 헬스 상태(`/kgs/{kg}/health`)와는 다른 경로예요 | 로그인 없이 | - | self-observability 5.1 |

진단, 운영 액션과 그 승인, 기동 중의 상태는 [추가 기능의 경로](rest-api-resources-additions.md) 4절에 있어요. 이벤트의 `kind` 목록과 형식은 rest-api.md 3-10이에요.

## 6. 알려진 한계와 설계 담당에게 확인할 사항
- 문제 목록의 `open-proposal`처럼 "문제에서 제안을 연다"는 편의 경로는 원천 문서의 절차에서 도출한 거예요. 새 방침이 아니에요.
- 지표, 목표, 알림을 KG를 선택하는 필드로만 지정하는 것(경로가 `/v1/...`)과 프락시움 자체 구성 요소의 헬스 상태를 시스템 KG의 헬스 상태 경로로 읽는 것은 설계 담당 확인이 필요해요.
- 알림의 확인과 에스컬레이션의 액션 이름(`acknowledge`, `snooze`, `hand-over`)은 상태 이름을 따라 지었어요.
- 설정에서 어떤 필드가 수정할 수 있는 설정인지는 정하지 않았어요.
- 한 객체를 직접 수정하는 경로는 없어요. KG는 정의와 원본 레코드의 결과예요(ADR-0009).

## 7. 관련 결정과 요구사항
- ADR-0016. 원천: ADR-0009(저장, 세대), ADR-0010(질의), ADR-0011(운영), ADR-0012(권한), ADR-0013(관측), ADR-0015(역량 질문), ADR-0017(프락시움 자신의 운영).
- REQ-143, REQ-147, REQ-148, REQ-149.
