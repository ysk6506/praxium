---
title: ADR-0075 정의의 id 문법과 스키마 버전
kind: decision
status: active
enforced_by:
  - backend/internal/ingest/idgrammar_test.go
  - backend/cmd/praxium/ingestidgrammar_test.go
  - TRACE006
updates_docs:
  - docs/guides/ingest-definitions.md
  - docs/design-docs/sources-and-adapters.md
  - docs/product-specs/requirements-definitions.md
touches_protected:
  - docs/product-specs/requirements-definitions.md
  - harness/config/test-lock.json
amends: []
amends_tests:
  - go:backend/internal/ingest::TestEveryCodeOfTheTableIsGivenByACaseAtItsPlace
test_change_reason: 새 오류 코드 9개의 사례 10건을 사례 목록에 더했다. 이 테스트는 표의 모든 코드에 사례가 있는지 확인하므로 새 코드를 더하면 사례도 더해야 한다. 기존 사례와 단언은 그대로이고 지운 줄이 없으며 사례가 줄지 않았다.
decision_hash: aeb04c5b09a1a667
---
# ADR-0075 정의의 id 문법과 스키마 버전

## 맥락

[ADR-0058](0058-sources-from-db-and-api-worker-ingestion-and-editing-screens.md)의 단계 1b다. 뒤의 단계는 소스와 매핑을 정의 저장소의 항목으로 두고, 여러 소스의 문서를 합쳐 데이터셋을 `<소스 id>.<데이터셋 id>`로 가리키고, 화면이 id로 항목을 가리킨다. 그러려면 문서 안의 모든 id가 하나의 문법을 따르고 문서에 스키마 버전이 있어야 한다. 지금 코드는 id에 빈 값만 거절하고(피드와 링크 정의의 id는 빈 값도 받는다) 문서에 버전이 없다.

## 결정

1. **id의 문법** `[a-z][a-z0-9_-]{0,63}`: 영문 소문자로 시작하고 영문 소문자, 숫자, `_`, `-`만 쓰며 64자까지다. 소스, 데이터셋, 매핑, 정제, 피드, 링크 정의의 `id`에 적용한다. 검사는 `core.ingest`의 `ValidID`와 `IDProblem` 한 곳에 있고 뒤의 단계가 쓴다. 어긋나면 정의 오류이고 다른 오류와 함께 한 번에 나온다. 문장은 허용하는 글자와 길이를 말하고, 값이 길면 잘라 싣는다.
2. **코드.** 비어 있는 id는 이미 있는 `….id_missing`이고 뜻을 넓히지 않는다. 비어 있지 않은데 어긋나면 새 코드 `source.id.invalid`, `source.dataset.id_invalid`, `mapping.id.invalid`, `mapping.refinement.id_invalid`, `mapping.feed.id_invalid`, `mapping.link.id_invalid`다. 피드와 링크 정의는 빈 id를 받던 것을 거절하므로 `mapping.feed.id_missing`, `mapping.link.id_missing`을 더한다.
3. **스키마 버전.** 소스와 매핑 문서의 맨 위에 `"version"`(정수)을 둔다. 없으면 1이다. 1이 아닌 정수(0과 음수 포함)는 정의 오류 `definition.unsupported_version`(kernel 코드 `unsupported`)이고 문장에 이 빌드가 읽는 버전을 적는다. 정수가 아닌 값은 해석 단계의 `definition.wrong_kind`다. 모르는 필드는 지금처럼 거절한다.
4. **매핑의 `id`.** 선택 필드다. 있으면 문법을 검사한다. 없는 문서는 지금처럼 읽는다. 컴파일한 `Plan`은 `Mapping`을 그대로 들고 있어 `Plan.Mapping.ID`로 읽는다.
5. **도구.** `ingest init`의 예제와 `ingest inspect -emit-source`의 초안에 `"version": 1`을 넣는다. 초안의 id는 파일과 시트 이름에서 만들므로 64자로 자르고(번호를 붙여도 넘지 않는다) 문법에 맞게 만든다.
6. 피드와 정제가 데이터셋을 가리키는 `dataset`, `input`의 값은 그대로다.

## 지금 받던 것 가운데 거절하게 되는 것

| 자리 | 전 | 지금 |
|---|---|---|
| 소스, 데이터셋, 정제의 id | 비어 있지 않은 모든 글자 | 대문자, 점, 공백, 한글, `/`, 숫자·`_`·`-`로 시작, 65자 이상을 거절한다 |
| 피드, 링크 정의의 id | 빈 값을 포함한 모든 글자 | 위와 같고 빈 값도 거절한다 |
| 두 문서의 `version`, 매핑의 `id` | 모르는 필드라 거절 | 읽는다 |

어긋나는 옛 id를 고치면 수집 상태가 소스 id와 데이터셋 id를 키로 하므로 옛 상태와 이어지지 않는다(객체가 다시 만들어지고 옛 객체는 아무도 닫지 않는다). 안내서에 적는다. 이 저장소의 잠긴 테스트와 예제에는 어긋나는 id가 없어서 고친 테스트는 없다.

## 이유

문법이 하나면 점을 `<소스 id>.<데이터셋 id>`의 구분자로 쓸 수 있고(데이터셋 id에 점이 없다), URL과 파일 이름에 그대로 쓸 수 있다. 버전을 지금 넣어 두면 뒤에 형식이 바뀔 때 옛 문서를 가려 읽을 수 있다. 하이픈을 허용하는 것은 손으로 쓰는 id에 흔하기 때문이다.

## 대안과 기각 사유

- 대문자와 점을 허용하고 나중에 막기: 이미 쓰인 id가 늘어 막기가 더 어려워진다. 지금이 가장 싸다.
- 빈 id의 오류를 `….id_invalid`로 합치기: 이미 있는 코드의 뜻이 달라진다. 코드는 바꾸거나 재사용하지 않는다(ADR-0070).

## 결과

- 이 변경에 넣지 않은 것: 정의 저장소의 종류(8a), 여러 소스의 계획과 `<소스 id>.<데이터셋 id>` 가리키기(6b), 어댑터 종류의 확장(4a), 시크릿의 id 코드.
- 오류 코드의 표에 코드 9개가 늘었다.
