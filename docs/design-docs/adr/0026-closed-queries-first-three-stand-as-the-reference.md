---
title: ADR-0026 사전 정의 질의의 첫 3개가 기준 구현이 된다
kind: decision
status: active
enforced_by:
  - GOARCH010
  - GOARCH011
  - GOARCH052
  - GOTEST002
  - GOCOV001
  - TRACE006
  - backend/internal/query/find_test.go
  - backend/internal/query/get_test.go
  - backend/internal/query/neighbors_test.go
  - backend/internal/query/permission_test.go
updates_docs:
  - ARCHITECTURE.md
  - docs/design-docs/kg-queries.md
  - docs/exec-plans/active/plan.md
  - docs/product-specs/requirements.md
touches_protected:
  - harness/config/architecture.yaml
  - harness/config/coverage-floor.json
  - docs/product-specs/requirements.md
decision_hash: 4f01802c5c90d8bd
---
# ADR-0026 사전 정의 질의의 첫 3개가 기준 구현이 된다

## 맥락

ADR-0025가 스토리지 포트와 메모리 기준 구현을 세웠다. 알파 1의 한 줄(소스에서 KG, `find`/`get`/`neighbors`, API와 명령줄)이 그 위에서 동작한다. `kg-queries.md`는 13개 질의의 필드와 엔벨로프를 정했지만 코드는 없었다. 이 결정은 3단계를 한 ADR에 적는다: **2부** 질의 단위(`core.query`), **3부** 경로와 계약과 CLI, **4부** 개발용 적재와 측정. 문서가 비었거나 어긋난 곳은 `kg-queries.md`와 `rest-api.md`에 가장 부합하는 해석을 골라 아래에 적는다.

## 결정: 2부, 질의 단위

1. **단일 단위.** `core.query`(`backend/internal/query`)는 `find`, `get`, `neighbors`를 스토리지 포트(`Source`: `GetKG`, `Snapshot`, `Versions`, `WaitReadable`) 위의 느리지만 단순한 구현으로 제공한다. 인덱스 없이 스냅샷을 전체 스캔한다. 이후의 엔진이 같은 응답을 내야 하는 기준이다. 운영 코드는 `core.kernel`, `core.model`, `core.graph`만 import한다. 테스트가 테스트 데이터를 구성하느라 `adapter.memstore`를 import하므로 구조 등록의 `may_import`에 이름이 올라간다(테스트 전용이다. 시계, 난수, 환경은 참조하지 않는다).
2. **읽기 워터마크.** 입력의 우선순위는 커서 > `kg_version_id` > `change_seq` > 최신(`readable_through`)이다. 버전과 변경 번호를 함께 지정하면 거절한다. `min_seq`는 `wait_timeout`까지 `WaitReadable`로 기다린 뒤 최신을 읽는다. 과거 시점 읽기는 `volatile_as_of: omitted`이고 값을 반환하지 않는다. `as_of.sealed_version`은 읽기 워터마크가 확정 버전의 변경 번호와 같으면 채운다.
3. **커서.** 커서는 세대, 변경 번호(버전이면 버전 ID), 질의의 지문, 재개 위치(`find`는 객체 ID, `neighbors`는 홉과 링크 ID)를 포함하는 불투명 문자열이다. 다음 페이지는 언제나 그 변경 번호로 읽어서 중간의 변경이 페이지를 흔들지 않는다(그 페이지는 과거 시점 읽기라서 `volatile` 값이 없다). 다른 질의의 커서는 `query_cursor_invalid`, 세대가 달라졌으면 `graph_generation_changed`다.
4. **`shape`.** 문서의 "건수와 그룹 먼저, 기본"을 문자 그대로 해석한다: `summary`는 건수와 그룹(`counts_by_type`, `groups`)만, `items`는 거기에 항목을 페이지 단위로 추가한다. 목록 경로(GET)는 기본값을 `items`로 둔다(3부).
5. **엔벨로프.** 채우는 것: `as_of`, `volatile_as_of`, `served_by.role = api_sql`, `freshness`(사용된 값 가운데 가장 오래된 관측 시각과 그 출처. 소스마다 기대 주기가 없어 상태는 `no_expected_period`), `evidence`(등급별 건수, 가장 낮은 등급, 근거의 종류, 가장 오래된 관측의 나이), `result_complete`, `hidden`, 빈 `warnings`와 `model_limits`. 채우지 않는 것: `health_hint`(헬스 상태 데이터가 없다), `served_by.built_at`(프로젝션이 없다). 사용되지 않은 값(숨김 처리됐거나 한도보다 오래된 값)은 신선도와 근거에 집계하지 않는다.
6. **관점과 한도.** `as_designed`는 근거의 종류가 `configured`와 `declared`인 속성과 링크만 대상으로 하고, `as_observed`(기본)는 모두 대상으로 한다. 행이 값을 하나만 보유하므로 "관측이 있으면 관측, 없으면 설계"는 보유한 값 그대로다. `max_staleness`는 `get`에서 관측 시각이 그보다 오래된 속성을 `stale: true`로 표시하고 값을 반환하지 않는다.
7. **읽기 권한 포트.** `ReadFilter`는 객체 하나에 대해 `(보임 여부, 민감 등급의 한도)`를 반환하고(`access-control.md` 5-4의 3요소 조합, 한도는 해당하는 부여들의 최댓값), 범위 조건이 사용하는 `scope_keys` 값이 필요한지, 모든 보이는 객체가 허용하는 등급의 하한(`Floor`), 숨김 건수를 정확히 제공할지(`Exact`)를 알려 준다. `AllowAll()`과, 부여의 3요소 조합을 해석하는 `Grants`(테스트 더블이자 API가 역할을 부여로 변환할 때의 계산)를 제공한다. 타입 선택자는 하위 타입을 포함한다. 한도를 넘는 속성은 `masked`/`sensitivity_above_limit`로 표시된다. 숨김 객체의 `get`은 없는 객체와 같은 `query_object_not_found`다. 링크는 양 끝이 보일 때만 반환한다. 텍스트 검색은 호출자가 읽을 수 있는 속성만 대상으로 한다.
8. **숨김 건수.** `find`는 찾은 것이 숨김이면 `hidden_reached`, `neighbors`는 순회 경로의 마지막 홉에서 만난 숨김 객체가 `hidden_reached`, 그 앞 홉에서 만나 그 너머로 순회한 것이 `hidden_passed`다. 순회는 전체 그래프에서 하고 필터링은 항목을 만들 때 한다. 숨김 객체를 경유한 항목의 `via_object_id`는 비고 `via_hidden`이 참이다. 전체 범위가 아닌 호출자에게는 건수가 `null`이고 `hidden_passed`/`hidden_reached`(참/거짓)만 있다.
9. **거절하는 입력**(닫힌 코드는 `query.Codes()`, 괄호는 3부가 변환할 REST 코드): 숨김 속성의 조건(`query_hidden_filter`, `permission.hidden_attribute`), `volatile` 속성의 조건(`query_volatile_filter`, `invalid_input.filter_field_unknown`), `scope_id`(`query_scope_unsupported`, `invalid_input.param_invalid`), 알 수 없는 필드와 연산, 잘못된 커서와 한도, 알 수 없는 `include`. 숨김 속성의 판정은 호출자 부여들의 가장 낮은 한도(`Floor`)를 기준으로 하고, 객체별 한도도 다시 확인한다(값이 한도를 넘는 객체는 그 조건에 맞지 않는다).
10. **포트 확장.** `get`의 `identifiers`는 객체에서 식별자를 조회하는 경로가 필요한데 포트에는 값에서 객체를 조회하는 경로뿐이다. 선택 기능 `graph.IdentifierLister`(`IdentifiersOf`)를 스냅샷이 구현할 수 있게 하고 메모리 어댑터가 구현한다. 필수 인터페이스를 늘리지 않은 것은 ADR-0025의 테스트 더블이 필수가 되면 깨지기 때문이다. 운영 어댑터는 객체별 인덱스로 구현해야 하고, 구현하지 않으면 `identifiers` 포함은 `unsupported`다.
11. **문서가 어긋나서 선택한 것.** `access-control.md` 7-3의 `masked_attribute_in_condition`과 `rest-api-resources-graph.md`의 `permission.hidden_attribute`가 다르다. 계약의 목록(`rest-api.md` 3-9)이 닫혀 있으므로 후자를 사용한다. 속성 조건의 값은 문자열로 비교하고 숫자는 숫자로(범위에서 양쪽이 숫자일 때), 시각은 RFC 3339 문자열로 비교한다.

## 이유

- 질의의 의미가 하나의 느린 구현에 있어야 빠른 엔진이 대조할 기준이 생긴다(`kg-queries.md` 7절).
- 읽기 권한을 포트로 두면 질의가 부여의 구조를 모르고, API가 역할을 부여로 변환하는 일은 어댑터의 일로 남는다.
- 커서가 읽기 워터마크를 포함하면 페이지 도중의 변경이 페이지를 건너뛰거나 중복되게 하지 않는다.

## 대안과 버린 이유

- 질의가 포트가 아니라 `memstore`를 직접 읽기: 두 번째 스토리지가 들어오면 다시 작성해야 한다.
- `shape: summary`에도 항목을 주기: 허브에서 `groups`만 반환하라는 문서와 `items`를 따로 둔 이유가 사라진다.
- 숨김 속성의 조건을 조용히 필터링: 존재 여부로 값이 노출된다(`access-control.md` 7-3).

## 한계

- 전체 스캔이라 느리다: `find`는 페이지마다 스냅샷 전체를 스캔한다(건수를 모든 페이지에서 같게 반환하려고). 측정은 4부에 있다.
- 범위 조건(`object_conditions`)의 판단은 포트와 테스트 더블에 있고 API가 부여를 변환하는 일은 클로즈드 베타다.
- 신선도 상태 `fresh`/`stale`은 소스의 기대 주기가 생기면 채운다. `containment`, `paths`, `impact`와 나머지 질의는 이 변경에 포함되지 않는다.
- 이 ADR은 3부와 4부를 같은 변경에서 이어서 적는다.

## 요구사항 테이블의 변화

- **분리했다**: REQ-140에서 REQ-250(`find`), REQ-251(`get`), REQ-253(읽기 권한 필터링), REQ-254(`neighbors`); REQ-140, REQ-146, REQ-222, REQ-232에서 REQ-252(엔벨로프, 읽기 워터마크, 보는 기준). 남은 REQ-140, REQ-146, REQ-222, REQ-232는 예정으로 남아 있다.
