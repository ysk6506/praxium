---
title: ADR-0070 정의의 오류를 위치와 코드와 함께 여러 개 낸다
kind: decision
status: active
enforced_by:
  - backend/internal/ingest/deferror_test.go
  - backend/cmd/praxium/ingesterrors_test.go
  - TRACE006
updates_docs:
  - docs/guides/ingest-definitions.md
  - docs/product-specs/requirements-ingestion-usability.md
touches_protected:
  - docs/product-specs/requirements-ingestion-usability.md
  - harness/config/test-lock.json
amends: []
amends_tests: []
decision_hash: 588032ba6d94e48c
---
# ADR-0070 정의의 오류를 위치와 코드와 함께 여러 개 낸다

## 맥락

소스 정의와 매핑 정의의 검사(`ParseSource`, `ParseMapping`, `Compile`)는 첫 오류에서 멈추고, 오류가 영어 문장 하나이고, 위치가 문장 속에 들어 있으며, 코드는 `invalid_value`와 `unsupported` 둘뿐이다. 정의를 화면에서 편집하려면 화면이 오류를 필드 옆에 보여야 하고, 한 번의 검사로 고칠 것을 모두 알아야 한다. 문장은 REQ-327, REQ-385, REQ-389 등으로 테스트에 잠겨 있다(ADR-0046, ADR-0055).

## 결정

1. **오류의 모양.** 오류 하나(`ingest.DefinitionError`)는 `document`(`source`, `mapping`), `pointer`(그 문서 안의 JSON 포인터, RFC 6901, 목록의 항목은 0부터 센 순번), `code`, `params`(문장을 만든 값: 준 값, 후보 목록, 기대한 종류. JSON으로 나갈 수 있는 값만), `message`(지금의 영어 문장 그대로)를 가진다.
2. **무관한 오류는 모두, 따라 나오는 오류는 생략.** 검사는 한 피드의 오류가 다른 피드의 검사를 막지 않게 하고, 같은 피드 안에서도 서로 필요로 하지 않는 검사는 모두 한다. 앞의 오류가 있어야 뜻이 있는 검사는 건너뛴다: 데이터셋이 없는 피드의 컬럼 검사, 타입이 없는 피드의 속성 이름과 키 검사, 해석하지 못한 링크 타입의 속성 검사, 거절된 정제를 읽는 피드, 지원하지 않는 파서의 시트와 구분자 검사. 차례는 문서의 항목 차례(데이터셋, 정제, 피드, 링크의 순번)이고 한 항목 안에서는 검사가 일하는 차례다. 맵의 순회 차례에 기대지 않는다. 한 번에 100개까지 내고(`MaxDefinitionErrors`), 넘으면 `Omitted`가 뺀 수를 센다.
3. **해석 단계.** JSON이 깨짐, 모르는 필드, 값의 종류가 틀림, 문서가 둘, 읽기 실패도 같은 모양이다(코드는 `definition.*`). 디코더가 첫 문제에서 알려 주므로 이 단계는 오류 하나이고, 실패한 문서의 그 뒤 검사는 하지 않는다.
4. **문장은 바뀌지 않는다.** 문장을 고치고 싶은 곳이 있어도 이 변경에서는 고치지 않는다. 정제의 연산 오류 문장이 안쪽 오류의 코드 단어(`invalid_value:`, `unsupported:`)를 품는 것도 그대로다.
5. **코드의 이름과 표.** 코드는 점으로 나눈 소문자와 밑줄이고 문서(`source`, `mapping`, 해석 단계는 `definition`)로 시작한다. 이름을 바꾸거나 다시 쓰지 않는다. `core.ingest`의 `ErrorCodes()`가 모든 코드와 영어 한 줄 설명, 대응하는 kernel 코드, `params`의 키를 가진 표를 낸다. 검사는 표의 행을 직접 받아 오류를 만들므로 표에 없는 코드는 낼 수 없다. 테스트가 이름 규칙과 겹침, 표의 모든 코드를 내는 사례(위치까지), 오류의 `params` 키가 표에 있음을 확인한다.
6. **호환.** `*DefinitionErrors`가 `error`를 만족한다. 오류가 하나이면 `Error()`는 전과 같은 `코드: 문장`이고, 여럿이면 한 줄씩이다. kernel 코드(`kernel.CodeOf`, `errors.As`)는 **첫 오류의 것**이다. 섞이면(첫째가 `unsupported`, 둘째가 `invalid_value`) 첫째를 따라 `unsupported`다. 종료 코드는 `ingest run`에서 어느 쪽이든 1이다. 목록은 `ingest.DefinitionErrorsOf(err)`로 꺼내고, 두 문서의 오류는 `ingest.JoinDefinitionErrors`로 잇는다.
7. **CLI.** `ingest run`은 소스와 매핑을 둘 다 읽어 보고 오류가 하나 넘으면 줄마다 하나씩 출력한 뒤 개수 줄을 낸다. 하나이면 출력은 전과 같다. 자연 키 후보(`withKeyCandidates`)는 오류마다 붙인다. 새 명령도 새 플래그도 없다.

## 이유

화면의 필드 옆 오류와 코드별 안내(나중에 한국어 문장을 코드로 고르는 일)에는 위치와 코드가 있어야 하고, 한 번에 모두 알려야 정의를 고치는 왕복이 줄어든다. 문장을 그대로 두면 잠긴 테스트와 안내서의 인용이 그대로 맞는다. 표를 코드에 두고 검사가 그 행을 받게 하면 표에 없는 코드가 생길 길이 없다.

## 대안과 기각 사유

- 오류를 문자열 목록으로 한 번에 내기: 위치와 코드가 없어 화면이 쓸 수 없다.
- 해석 단계에서도 모르는 필드를 모두 찾기: 문장이 디코더의 첫 오류에 기대므로 이 변경에서는 하나로 둔다.
- 코드를 문장 속 어구로 두기: 문장을 고치면 코드가 깨진다.

## 결과

- 오류가 둘 이상인 입력은 전에는 첫 오류만 나왔고 지금은 모두 나온다. 잠긴 테스트는 하나도 고치지 않았다(`amends_tests`가 비어 있다).
- 이 변경에 넣지 않은 것: REST로 내기와 `/v1/catalog/error-codes`, 한국어 문장, id의 문법, 문서의 판 번호, 소스 정의의 새 필드, 새 CLI 명령. 해석 단계의 오류를 여럿 내는 것도 뒤로 미룬다.
