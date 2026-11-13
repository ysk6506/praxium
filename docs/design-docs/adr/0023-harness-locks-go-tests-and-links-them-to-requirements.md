---
title: ADR-0023 하네스가 Go 테스트를 잠그고 요구사항에 잇는다
kind: decision
status: active
enforced_by:
  - TLOCK001
  - TLOCK002
  - TLOCK003
  - TLOCK004
  - TRACE002
  - TRACE006
  - TRACE007
  - GOTEST003
  - harness/src/checks/go-decls.test.ts
  - harness/src/checks/go-test-units.test.ts
  - harness/src/checks/go-lock.test.ts
  - harness/src/checks/go-traceability.test.ts
  - harness/src/checks/quality-go.test.ts
updates_docs:
  - AGENTS.md
  - docs/design-docs/harness-guide.md
  - docs/design-docs/adr-process.md
  - docs/exec-plans/active/plan.md
  - docs/exec-plans/tech-debt.yaml
  - docs/product-specs/requirements.md
touches_protected:
  - AGENTS.md
  - harness/**
  - docs/product-specs/requirements.md
  - docs/design-docs/adr/0019-harness-reads-go-backend-place-and-checks.md
amends:
  - adr: "0019"
    what: Go 테스트 잠금과 추적과 등급표
amends_tests:
  - harness/src/test-support/go-repo.ts::=BASE
test_change_reason: Go 테스트에도 "실패를 부를 길이 없는 테스트는 안 된다"(GOTEST003)가 적용되어, Go 하네스 테스트의 작은 Go 모듈 샘플(go-repo.ts)에서 cmd/app의 테스트가 로그만 남기던 것을 실제로 실패할 수 있는 테스트로 바꾼다. 샘플의 뜻은 그대로이고 요구사항이 아닌 샘플 데이터만 바뀐다.
decision_hash: b0f80c89d28d385a
amended_by:
  - adr: "0028"
    what: Go 테스트 규칙의 이식과 잠금의 지문 범위
---
# ADR-0023 하네스가 Go 테스트를 잠그고 요구사항에 잇는다

## 맥락
ADR-0019가 하네스에 Go 코드를 읽는 검사를 만들었지만 TypeScript 테스트에만 있는 세 가지 보호는 Go에 넘기지 못했다(부채 TD-0008): 테스트를 지우거나 약화하거나 기대값을 바꾸는 것을 막는 테스트 잠금(`TLOCK*`), 요구사항 추적(`TRACE*`), 품질 등급표. 그동안 Go 테스트를 고치는 변경은 리뷰가 지켰다. 표준 모델과 세계 생성기의 테스트는 골든 상수(해시, 개수)가 많아 이 구멍이 실제로 위험하다(ADR-0022가 그 값을 손으로 적어야 했다). 설계 담당이 방향을 정했다: 세 보호를 Go로 넓히되 이미 있는 어떤 것도 약하게 하지 않는다.

## 결정
1. **잠금 단위는 최상위 테스트 함수다.** `TestXxx`, `FuzzXxx`, `ExampleXxx` 하나가 잠금 항목 하나이고, 키는 `go:<패키지 디렉터리>::<함수 이름>`이다(`go:backend/internal/model::TestStandardCounts`). TypeScript 키는 `경로::이름`이라 `go:`로 시작하는 키와 겹치지 않는다. 테스트를 같은 패키지의 다른 파일로 옮겨도 키는 그대로이다. 항목은 다른 테스트와 같은 모양(`kind: test`, `hash`, `asserts`, `literals`, `skips`)이고 Go만 `reqs`(달린 요구사항 태그)를 더 갖는다.
2. **지문.** 함수의 토큰, 그리고 그 함수가 이름으로 이어서(되풀이해서) 닿는 같은 패키지의 `_test.go` 안 함수, 메서드, 타입, 변수, 상수(공유 표와 골든 값 포함), `init`과 `TestMain`과 `var _`(늘 포함), `//go:embed`로 가져오는 파일의 내용, 본문의 문자열이 가리키는 `testdata/` 아래 파일의 내용, 달린 `[REQ-NNN]` 태그를 섞은 해시다. 서식, 줄 바꿈, 주석, 후행 쉼표는 토큰으로 정규화하므로 지문에 들지 않는다. 테스트를 더하는 것은 다른 항목의 지문을 바꾸지 않는다. `backend/**/testdata/**`는 `tests.yaml`의 `data_globs`에 더해 파일 하나하나도 데이터 항목으로 잠긴다.
3. **풀리는 길.** 지우거나 이름을 바꾸면 `TLOCK002`(그리고 새 이름은 `TLOCK003`), 바뀌면 `TLOCK001`이다. 결정 기록의 `amends_tests`에 키(`go:디렉터리::함수`)를 적고 확정한 뒤 `pnpm lock-tests --amend <ADR>`로만 풀린다(`TLOCK004`가 확정과 목록을 본다). 풀이는 TypeScript와 같다.
4. **약화 규칙은 TypeScript를 Go로 옮긴 것이다.** 실패를 부르는 호출의 수(`t.Error`와 `t.Fatal` 가족, `t.Fail`, `t.FailNow`, 그리고 그것을 부르는 도우미를 쓴 곳 하나당 하나), 건너뛰기(`t.Skip` 가족)의 수, 닿는 모든 리터럴의 지문, 본문이 비었는지. 이 값이 항목에 적히고, 바뀌면 `TLOCK001`의 설명이 어느 쪽이 약해졌는지 밝힌다. 새 테스트가 실패를 부를 길이 없으면(`Example`은 `// Output:` 주석이 길이다) `GOTEST003`이다(TypeScript의 `TEST001`).
   - **TypeScript에 없는 것을 더한 규칙은 둘이다**(둘 다 같은 뜻의 Go 꼴일 뿐이다): `testing.Short()`로 테스트를 가르는 것을 건너뛰기로 센다. 그리고 요구사항 태그의 바뀜도 지문에 든다.
   - **옮기지 않은 것**: `TEST003`(상수가 판정을 결정함), `TEST004`(넓은 catch), `TEST005`(`.only`)의 Go 꼴. 설계 담당이 테스트, 빈 본문, 실패 호출 없음만 옮기라고 했다.
5. **요구사항 추적.** `Test` 함수 바로 위 주석에 `// [REQ-NNN] [REQ-MMM]`을 적고, 부분 테스트 이름(`t.Run`의 문자열 인자)에도 태그를 둘 수 있다. 이 태그가 `TRACE001`(테스트 없는 요구사항), `TRACE002`(없는 id), `TRACE006`(테스트가 인용하는 `예정` 요구사항)에서 TypeScript의 `[REQ-NNN]`과 같이 센다. 새 규칙 `TRACE007`: 테스트 파일의 주석에서 `[REQ-`로 시작하지만 어디에도 잇지 못하는 태그(본문 안 주석, 도우미 위 주석, 한 괄호에 여러 id)는 연결인 척하는 가짜라 실패한다. 세 요구사항을 위해 아래에 적은 대로 기존 Go 테스트에 태그를 달았고, 본문 안에 있던 옛 비공식 태그는 모두 걷었다.
6. **품질 등급표.** Go 단위는 `go:<단위>` 행이다(`core.kernel`이 두 언어에 있어도 두 행이다. 커버리지 하한 키와 같은 이름). 등급 기준은 같다. 입력은 `go test`의 문장 커버리지(`pnpm quality record`가 기록, 없으면 "알 수 없음"이고 등급은 올리지도 내리지도 않는다), Go 파일과 함수의 크기와 복잡도(`debt.yaml`의 한도), 끄기(`GOIGN*`)의 수, 열린 부채(대장의 `units`에 `go:<단위>`), 문서다. 부채 대장의 `units`에는 `go:` 이름도 쓸 수 있다(`QUAL003`).
7. **구현.** Go를 읽는 쪽이 이미 쓰는 확장 토크나이저(`go-lex.ts`)로 선언을 나눈다(`go-decls.ts`, `go-test-units.ts`). `go/ast`를 부르는 `go run` 도우미는 만들지 않았다: 테스트가 빠르고 Go 캐시가 비어도 돌며, 새 의존성이나 도구 호출이 없다. 이 검사들은 이미 있는 검사 이름(`test-integrity`, `coverage`, `traceability`, `quality`) 안에서 돈다(검사 이름의 목록은 `common.test.ts`가 잠근다).
8. **첫 잠금.** 있던 Go 테스트 함수 모두를 잠그고(항목만 더했고 TypeScript 항목은 하나도 바뀌지 않았다), 요구사항 표를 아래처럼 고쳤다.
9. **부채 TD-0008을 닫는다.** 해결: 이 결정이 만든 검사.

## 한계
- 이름으로 닿는 곳을 찾아서 지문은 넓게 잡힌다. 지역 변수가 패키지의 변수와 이름이 같으면 그 변수가 닿은 것으로 센다. 그래서 잠금은 깐깐해질 뿐 느슨해지지 않는다: 도우미를 고치면 그것을 쓰는 모든 테스트가 `TLOCK001`로 걸리고 모두 `amends_tests`에 적어야 한다.
- 다른 패키지(`_test` 패키지를 포함)의 도우미, 컴파일러만 아는 타입 해석, 별칭으로 가린 호출은 보지 않는다. 테스트 밖 제품 코드가 바뀌어 기댓값의 뜻이 달라지는 것은 테스트가 실패해서 알려 준다(그것이 테스트의 일이다).
- `testdata/`가 아닌 곳의 파일은 `//go:embed`로 닿을 때만 지문에 든다.
- 첫 잠금이 항목 수를 정한다. 테스트를 더하면 `pnpm lock-tests`가 필요하다(`TLOCK003`). 이것은 TypeScript와 같다.

## 요구사항 표의 변화
- **예정을 풀었다**(Go 테스트가 이제 인용하고 테스트가 요구의 말을 실제로 지킨다): REQ-100(타입 등록부와 이름 규칙. `TestStandardCounts`, `TestStandardSpotChecks`, `TestLookups`, `TestSubtypeQueries`, `TestBindingsForAPair`, `TestBaseValueTypes`, `TestReadDefinitionsErrors`와 이름 꼴과 검증 테스트의 표 행), REQ-101(닫힌 뜻과 채널. `TestClosedListsHaveTheDocumentedSizes`, `TestMeaningSpecsAreSound`, `TestMeaningSpecsAreCopies`, `TestNotSpreadingMeanings`, `TestEveryBindingUsesAnAllowedMeaningAndChannel`, `TestStandardMountingAndServiceDependencies`, `TestStandardSpotChecks`와 검증 테스트의 표 행), REQ-233(번호, 별명, 해시에 ID. `TestNumbersAreAssignedDeterministically`, `TestNumbersSkipReservedAndNeverReuse`, `TestStandardNumbersAreStable`, `TestStandardNumbersAreUniqueAndStable`, `TestRenameKeepsIdNumberAndHash`, `TestHashFollowsMeaning`, `TestStandardIsDeterministic`와 번호 검증 행). 테스트가 인용하는 요구사항에 `예정` 표시가 남으면 `TRACE006`이 실패하므로 표시를 지웠다.
- **예정으로 남긴 것**: REQ-102, REQ-103, REQ-104, REQ-105, REQ-106, REQ-107, REQ-108, REQ-165, REQ-230. 요구가 말하는 동작(이중화의 세 값 판정, 사실·가정·알 수 없음 등급, 동일 객체 식별과 이력, 아홉 층의 표준 모델 전체와 덮어쓰지 않기, 근거의 종류, 세 층과 승인, 속성별 이력과 신선도, 읽기 권한의 객체 조건, 질의 엔진)을 아직 만들지 않았다. 구조 테스트가 일부분을 건드려도 요구 전체를 지키는 테스트가 아니라서 태그를 달지 않았다. 억지로 달면 `TRACE006`이 풀린 척이 된다.
- **REQ-233을 나눴다.** "옛 이름은 한 메이저 버전 동안 별명으로 받는다"는 테스트가 없어서(별명의 기한과 API의 받음은 아직 측정하지 못한다) 같은 말로 새 요구사항 REQ-241로 옮기고 `**예정(ADR-0016)**`(API 계약이 맡는다)로 표시했다. REQ-233은 테스트가 지키는 것(바뀌지 않는 번호, 다시 쓰지 않음, 이름을 바꿔도 ID와 번호와 해시가 그대로)만 남아 예정을 풀었다. REQ-100과 REQ-101은 문장마다 다시 견줬고 테스트가 닿지 않는 문장이 없어 나누지 않았다.
- 이 결정 이전의 ADR이 적은 Go 테스트 키(`파일::이름`, 예: ADR-0022)는 이 메커니즘보다 앞서서 그대로 둔다.
- REQ-023, REQ-024의 본문을 Go 보호가 생긴 것에 맞게 고쳤고(예전 문장 "아직 없다(TD-0008)"를 지웠다), 머리말에 Go 태그 규칙을 더했다.

## 이유
- 테스트 잠금은 테스트를 통과시키려고 테스트를 바꾸는 지름길을 막는 장치이고, 골든 값이 많은 Go 쪽이 더 필요하다. 함수 하나가 단위여야 "이 테스트는 이 요구사항을 지킨다"를 말할 수 있다.
- 지문이 도우미와 골든 값과 `testdata`를 따라가야 테스트 본문은 두고 기대값만 바꾸는 경로가 막힌다.
- 토크나이저를 쓰면 Go 도구를 부르지 않아 하네스가 단순하고 빠르다. `go/ast`가 더 정확한 곳은 위의 한계에 적었고, 정확함이 필요할 때 넘어갈 길은 남는다.

## 대안과 버린 이유
- **파일 하나를 단위로 잠그기**: 테스트 하나를 더해도 파일 지문이 바뀐다. 더하기가 막힌다.
- **`go run`으로 `go/ast`를 읽는 도우미**: 더 정확하나 첫 실행이 느리고 Go 캐시와 도구에 기댄다. 지금은 쓰지 않는다.
- **테스트 제목에서 태그를 읽기**: Go 테스트 제목은 함수 이름이라 태그를 넣을 곳이 없다.

## 결과
- TD-0008을 닫는다. 기존 Go 테스트 함수는 모두 잠겼고, Go 테스트를 바꾸려면 이 문서 틀의 결정 기록이 필요하다.
- 설계 담당에게 묻는다: (1) "테스트를 더해도 잠금이 깨지 않는다"와 `TLOCK003`(`pnpm lock-tests` 필요)의 관계를 TypeScript와 같게 두었다. (2) REQ-241(별명 기한)의 테스트는 API 계약이 생길 때 만든다.
