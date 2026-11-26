---
title: ADR-0113 질의 스키마 — 등급마다의 스키마와 역할, 뷰와 함수, 등급을 거르는 한 곳
kind: decision
status: active
enforced_by:
  - GOARCH010
  - GOCOV001
  - TLOCK001
  - backend/internal/pgschema/qsc_grade_test.go
  - backend/internal/pgschema/qsc_refuse_test.go
  - backend/internal/pgstore/qsc_query_test.go
  - backend/internal/pgstore/qsc_past_test.go
  - backend/internal/pgstore/qsc_schema_test.go
  - backend/internal/pgstore/qsc_installer_test.go
touches_protected:
  - docs/design-docs/adr/0085-new-kg-storage-implementation-decisions-and-the-schema-plan-unit.md
  - docs/design-docs/adr/0087-schema-executor-and-shared-tables.md
  - docs/product-specs/requirements-access.md
  - docs/product-specs/requirements-model-storage.md
  - docs/product-specs/requirements-query-api.md
  - harness/config/test-lock.json
updates_docs:
  - docs/design-docs/query-schema.md
  - docs/design-docs/storage-shape.md
  - docs/design-docs/postgres-operations.md
  - docs/product-specs/requirements-access.md
  - docs/product-specs/requirements-model-storage.md
  - docs/product-specs/requirements-query-api.md
amends:
  - adr: "0085"
    what: 생성기 버전 8의 단계(고정 테이블 넷)와 스키마 계획이 질의 스키마의 뷰, 함수, 설명, 권한도 낸다는 것
  - adr: "0087"
    what: 실행기가 질의 스키마의 역할을 만들고 지우며 공용 마이그레이션 0008이 설치 토큰을 둔다는 것
amends_tests:
  - go:backend/internal/pgschema::TestControlReservesThePartitionPrefix
test_change_reason: 생성기 버전이 7에서 8이 되었다. 테스트는 단언을 한 글자도 약하게 하지 않고 버전 숫자만 맞춘다(버전 8이 더하는 단계는 그 버전의 테스트에 있다).
decision_hash: c31347ee203f2b63
---
# ADR-0113 질의 스키마 — 등급마다의 스키마와 역할, 뷰와 함수, 등급을 거르는 한 곳

## 맥락

ADR-0080이 사용자가 SQL로 KG를 묻는 질의 스키마를 정했고, ADR-0085와 ADR-0087이 저장 스키마의 계획과 실행기를 지었다. 질의 스키마(타입 뷰, 링크 뷰, 공통 뷰, 과거 시점 함수, 모델 설명, 등급마다의 스키마와 역할)는 아직 없었다. 이 변경이 그것을 짓는다. 질의 실행기(역할로 바꾸기, `search_path` 걸기), 그래프 함수, REST와 MCP는 뒤의 변경이다.

## 결정

1. **KG마다 질의 스키마 여섯 개**: 등급(`open`, `restricted`, `confidential`) 셋 곱하기 일반과 큰 적용 중용(`_w`) 둘이다. 이름은 `q_<kg_no>_<등급>[_w]`다. 일반 세트에서 높은 등급의 스키마는 낮은 등급과 정의 글이 다른 객체만 가진다. `_w` 세트는 등급마다 완전한 한 벌이다(큰 적용 중에만 쓰는 것이라 단순한 쪽을 고른다). `search_path`는 높은 것부터 겹친다(`pgschema.SearchPath`).
2. **등급을 거르는 한 곳**: 속성의 민감도를 읽는 코드는 `pgschema/qgrade.go`(`attrGrades`, `VisibleAttrNos`) 한 곳이다. 모든 뷰, 함수, 설명, 변경 기록은 여기서 나온 속성 번호 집합으로 거른다. 소스를 훑는 테스트가 다른 파일이 민감도를 읽지 않음을 잠그고, 권한 테스트가 `provenance`, `attribute`, `object_detail`, `<타입>_at`, `value_change`, `model_attribute`가 같은 집합으로 걸러짐을 확인한다. 같은 번호를 두 속성이 쓰면 더 민감한 쪽을 따른다.
3. **역할** `pq_<설치 토큰>_<kg_no>_<등급>`: `NOLOGIN NOINHERIT`이고 저장 스키마와 다른 KG의 스키마에 권한이 없다. 자기 등급과 낮은 등급의 질의 스키마(일반과 `_w`)의 `USAGE`, 뷰의 `SELECT`, 함수의 `EXECUTE`만 가진다. 설치 토큰은 DB마다 한 번 정해지는 12자리 16진수이고 새 공용 마이그레이션 `0008_install_token.sql`이 둔다(역할은 클러스터에 속하므로 DB 둘이 같은 이름을 만들지 않게 하려는 것이다, Q2). 토큰은 이름에 들어가므로 계획이 모양을 검사한다(`CheckInstallToken`).
4. **PostgreSQL 18의 사실**(확인했다): (가) `CREATEROLE`로 만든 역할은 만든 쪽이 `SET ROLE` 하지 못한다(만든 쪽에는 admin만 주어진다). 실행기가 역할마다 `GRANT <역할> TO CURRENT_USER WITH SET TRUE, INHERIT FALSE`를 한다. 설치 계정은 슈퍼유저가 아니라 DB 소유자에 `CREATEROLE`이면 되고 테스트가 그 계정으로 돈다(Q2). (나) 비슈퍼유저의 `REVOKE EXECUTE … FROM PUBLIC`은 내장 함수에 효과가 없다(경고만 난다, Q1). 그래서 위험한 내장 함수의 실행 권한을 회수하는 설치 단계는 이 변경이 짓지 않고 질의 실행기의 변경이 정한다.
5. **뷰와 함수**: 타입마다 뷰(이름은 타입 이름의 snake_case, 부모는 자기 표와 모든 하위 타입 표의 `UNION ALL`, D-8: 객체는 구체 타입의 표에만 있어서 한 객체가 한 번만 나온다), 링크 타입마다 뷰, 공통 뷰(`object`, `link`, `attribute`, `identifier`, `kg_version`, `change`, `value_change`), 모델 설명 뷰 넷, 함수 `object_detail(id)`(등급마다 하나), `<타입>_at(변경 번호)`. 모든 뷰와 컬럼과 함수에 `COMMENT`가 있다. 근거 등급은 글자(`fact`, `assumed`, `unknown`)로 보인다. `provenance`는 이름이 키이고 `g`, `b`, `t`, `r`이 안의 키이며 값은 넣지 않는다. 변동 속성은 `<이름>`과 `<이름>_at` 두 컬럼이다. 속성 이름이 `name`이면 컬럼을 따로 두지 않는다. 컬럼 이름이 겹치거나 63바이트를 넘거나 예약된 이름과 같으면 계획이 거절한다. 조직 속성은 이름 그대로 큰따옴표로 감싼 컬럼이다.
6. **함수의 권한**: 함수는 `SECURITY DEFINER`에 `search_path = pg_catalog, pg_temp`로 고정하고 `PUBLIC`의 실행 권한을 회수한다(질의 역할은 저장 스키마를 못 읽으므로 함수가 소유자의 권한으로 읽는다). 등급은 함수 안에 들어 있고 호출자가 올릴 수 없다. 뷰 안에서 부르는 시각 변환 함수 `public.praxium_ts`만 `PUBLIC`이 실행한다.
7. **`<타입>_at(n)`**: 변동 값과 상태가 없는 같은 컬럼을 낸다. `n`이 이력의 바닥보다 작거나 읽기 워터마크보다 크거나 `NULL`이면 빈 결과가 아니라 SQLSTATE `PXNRD`의 오류다(온전해 보이지만 온전하지 않은 답을 막는다).
8. **모델 설명 표 넷**(`model_type`, `model_attribute`, `model_link_type`, `model_link_rule`)은 저장 스키마의 고정 표이고 생성기 버전 8의 더하는 단계다(이미 있는 표에도 되풀이할 수 있게 `IF NOT EXISTS`다). 질의 스키마를 만드는 트랜잭션이 표를 비우고 다시 채운다.
9. **다시 다시 만들기와 삭제**: 스키마 관리기가 일을 할 때마다 질의 스키마 여섯 개를 지우고 통째로 다시 만든다(만든 것과 따라간 것이 같다, 테이블은 건드리지 않는다). KG를 지우면(`DeleteKG`, `DropStorageSchema`) 질의 스키마와 역할도 같은 트랜잭션에서 지운다.
10. **생성기 버전 8**: 두 커밋(B3-1과 B3-2)이 한 버전 안이다. 버전 7은 먼저 합쳐진 변경(ADR-0108)이 썼으므로 이 변경이 8로 올렸다.

## 이유

- 등급을 거르는 한 곳이 없으면 뷰, 함수, 변경 기록 가운데 하나가 높은 등급의 값을 새게 하기 쉽다. 한 곳과 그것을 잠그는 테스트가 값이 새는 길을 구조로 막는다.
- 역할을 클러스터가 아니라 DB에 속하게 만들 수 없으므로 이름의 토큰이 충돌을 막는다.
- 함수는 저장 스키마를 읽어야 하는데 질의 역할은 그것을 못 읽으므로 `SECURITY DEFINER`가 필요하고, 그래서 `search_path` 고정과 `PUBLIC` 회수가 함께 필요하다.

## 대안과 기각 사유

- 등급마다 같은 뷰를 다 두기: 뷰가 세 배가 된다. 일반 세트는 다른 것만 둔다.
- `object_detail`에 등급 인자: 호출자가 올릴 수 있는 인자가 되어 스키마의 사용 권한에 기대는 깨지기 쉬운 구조가 된다.
- 읽을 수 없는 변경 번호에 빈 결과: 온전해 보이지만 온전하지 않은 답이 된다.

## 결과

- 이름만 바뀐 모델은 모델 해시가 같아(이름은 해시에 들지 않는다) 스키마 관리기가 일을 하지 않으므로 뷰가 다시 만들어지지 않는다. 다른 변경과 함께 관리기가 돌 때는 모든 뷰가 새 이름으로 다시 만들어진다. 이름만 바뀐 경우에 다시 만드는 길은 아직 없고 [질의 스키마](../query-schema.md) 12절의 미결이다.
- 표준 모델의 KG 하나를 만드는 시간이 약 1.6초에서 약 2.7초로 는다(질의 스키마의 뷰 836문장, 약 1.4초: 뷰 0.86초, 권한 0.37초, 함수 0.15초).
- 질의 스키마를 쓰는 처리 경로(질의 실행기, `SET LOCAL ROLE`, `search_path`, 그래프 함수, REST와 MCP)은 뒤의 변경이다.
