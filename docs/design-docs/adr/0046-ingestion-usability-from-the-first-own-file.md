---
title: ADR-0046 자기 파일을 처음 수집할 때 조용히 틀리지 않는다 — 없는 시트와 컬럼, 키 오류, 인코딩, 구분자, 정의 초안
kind: decision
status: active
enforced_by:
  - GOARCH010
  - GOCOV001
  - TLOCK001
  - TRACE006
  - backend/internal/ingest/usability_test.go
  - backend/internal/model/hints_test.go
  - backend/internal/query/schemehint_test.go
  - backend/cmd/praxium/ingestusability_test.go
  - backend/cmd/praxium/ingestguide_test.go
updates_docs:
  - docs/design-docs/ingestion.md
  - docs/product-specs/requirements.md
touches_protected:
  - docs/product-specs/requirements.md
  - docs/design-docs/adr/0041-cli-usability-local-mode-and-keeping-ingestion-results.md
  - harness/config/test-lock.json
amends_tests:
  - go:backend/internal/httpapi::TestCatalogsServeTheClosedLists
test_change_reason: 카탈로그 목록(/v1/catalog)에 레이어 목록(layers)이 빠져 있어 명령줄에서 찾을 수 없었다. 목록에 넣어 항목이 여섯에서 일곱이 된다. 다른 단언은 그대로다
amends:
  - adr: "0041"
    what: ingest init의 CSV 예제가 합격 테스트의 샘플 파일과 같다는 결정
decision_hash: 0ff1c0654a2919a1
---
# ADR-0046 자기 파일을 처음 수집할 때 조용히 틀리지 않는다

## 맥락

릴리스 실행 파일(ADR-0039, ADR-0041)로 독립 검증자가 자기 엑셀을 넣어 보았다. 예제 경로는 막힘이 없었고 오류 메시지의 수준(셀 주소, 행 번호, 허용 값)도 좋았다. 그러나 자기 파일에서 곧바로 부딪히는 문제가 있었다. 원칙은 하나다. **조용히 틀리지 않는다.** 잘못된 입력은 무엇이 틀렸고 무엇이 가능한지 말해 준다.

1. 소스 정의가 가리키는 시트가 파일에 없을 때, 그 데이터셋의 줄은 모든 개수가 0이고 이유가 없어서 "문제 없이 비어 있는" 것처럼 보인다. 문제 목록에 `shape_changed`가 있어도 다른 줄에 묻힌다. 컬럼이 빠진 오류는 실제로 읽은 머리글을 알려 주지 않아 틀린 `header_row`를 알아채기 어렵다.
2. 식별 키 오류(`type Rack has no identity key made of the properties [name]`)는 허용되는 키를 알려 주지 않는다. 스킴 이름이 틀린 오류도 같다.
3. 예제 매핑은 `identifiers`를 채우지 않아 수집한 객체의 식별자가 0개이고, `--identifier`로 찾는 길이 보이지 않는다.
4. EUC-KR(CP949)로 저장한 CSV는 깨진 글자로 읽히고 경고가 없다. 세미콜론이나 탭으로 구분한 파일은 한 컬럼으로 읽힌다.
5. 쓰지 않는 컬럼도 모두 선언해야 `undeclared_column` 문제가 나지 않는다(컬럼이 30개인 대장에서 5개만 쓰는 경우).
6. 소스 정의를 처음부터 손으로 써야 한다.
7. 일부 오류 메시지(알 수 없는 필드, 비어 있는 자연 키, 서버에 묻지 않는 명령 앞의 전역 플래그)는 원인이나 고칠 방법을 말하지 않는다. `health`와 `types get`의 표 출력은 중첩 값이 `{...}`로만 나온다.

## 결정

**1. 없는 시트와 빠진 컬럼 (REQ-320).** 시트가 없거나 선언한 컬럼이 머리글에 없는 데이터셋은 기존대로 `shape_changed` 문제이고(문제 수에 잡히고 `-fail-on-problem`이면 종료 코드 20), 같은 이유가 보고의 `not_read`와 데이터셋 줄의 `not_read="..."`로 나온다. 시트가 없으면 파일에 실제로 있는 시트 이름을, 컬럼이 없으면 실제로 읽은 머리글(시트는 머리글 행 번호, 같은 병합 제목은 한 번만)을 메시지에 싣는다. 이 줄의 개수가 0인 것은 파일이 비어서가 아니라 읽지 않아서임이 보인다.

**2. 식별 키와 이름 오류 (REQ-321).** 모델에서 읽을 수 있는 목록을 메시지에 싣는다. 식별 키 오류는 타입의 모든 키(속성 조합, 링크가 낀 조합, 스킴)와 매핑에 쓰는 법을 말하고, 링크가 낀 키는 이 빌드의 매핑에서 쓸 수 없다고 밝힌다. 스킴 오류는 그 타입의 키인 스킴을 먼저, 모델의 모든 스킴을 뒤에 싣는다. 속성, 컬럼, 데이터셋 이름이 틀린 오류는 가능한 이름을, 객체 타입과 링크 타입이 틀린 오류는 비슷한 이름(대소문자와 밑줄을 무시해 같거나 두 글자 이내)을 싣는다. 질의의 `identifier.scheme` 오류도 스킴 목록을 싣는다(계약과 `Problem` 형식은 바꾸지 않고 메시지 문장만 길어진다). 목록과 키 설명은 `model`이 만든다(`IdentifierSchemeNames`, `SchemeKeyNames`, `DescribeIdentityKeys`, `PropertyNames`).

**3. 예제의 식별자 (REQ-322).** 엑셀과 CSV 예제는 서버에 `identifiers: [{"scheme": "bmc_mac", "column": "bmc_mac"}]`를 둔다(표준 모델에서 서버(PhysicalServer)의 키인 스킴 가운데 합성 데이터에 맞는 것). 랙은 표준 모델에 알맞은 스킴이 없어 식별자가 없다. 식별자를 질의에 쓰면 원장에 적힌 모양(콜론이나 하이픈이 든 MAC)이 저장된 모양(열두 자리 16진수)과 달라 찾지 못하므로, 질의의 `identifier` 조회(`query get`, `query find`)는 값을 스킴의 정규화로 맞춰 찾는다(정규화가 값을 거절하면 그대로 찾는다). CSV 예제는 합격 테스트가 쓰는 `testdata`의 사본(`ingestcheck/samples-csv`)이 되었다. 같은 파일을 쓰면 예제를 풍부하게 할 때마다 잠긴 테스트 데이터와 그래프 해시 단언이 바뀐다.

**4. CSV의 인코딩과 구분자 (REQ-323, REQ-324).**
- CSV와 JSON Lines 파일이 UTF-8이 아니면(올바르지 않은 바이트가 있거나 UTF-16 바이트 순서 표시로 시작하면) `ingest inspect`와 `ingest run` 모두 멈춘다. 오류는 무엇이 문제인지와 해결 방법("CSV UTF-8(쉼표로 분리)"로 다시 저장하거나 `.xlsx` 파일을 그대로 쓴다)을 말하고, `run`은 어느 데이터셋인지 말한다. UTF-8 BOM은 지금처럼 읽는다.
- CP949(EUC-KR)를 직접 읽지 않는다. 표준 라이브러리에 없고 `golang.org/x/text` 같은 서드파티가 필요하며, 의존성을 늘리지 않는 것이 제품의 방침이다(ADR-0040, REQ-285). 코드표를 직접 넣는 것은 2만 자가 넘는 표를 유지하고 검증하는 부담이다. 인코딩 자동 감지는 틀리면 조용히 깨진 데이터를 만드는 일이라 "조용히 틀리지 않는다"와 맞지 않는다. 명확한 오류와 해결 방법이 더 싸고 안전하다. 사용자는 엑셀에서 UTF-8로 다시 저장하거나 `.xlsx`를 그대로 쓰면 된다. 이 한계는 의도한 것이고, 수요가 크다고 확인되면 별도 결정으로 다시 본다.
- 데이터셋에 `delimiter`(쉼표가 기본, 탭, 세미콜론, 세로선을 문자나 `comma`, `tab`, `semicolon`, `pipe`로)를 둔다. CSV 파서에만 있다. `ingest inspect`에 `-delimiter`를 더한다. 쉼표로 읽어 한 컬럼이 되고 그 값에 탭, 세미콜론, 세로선이 반복되면 inspect가 "구분자가 다른 것 같다"고 알리고(`FileProfile.Notes`), 수집에서 컬럼이 빠진 오류에도 같은 안내가 붙는다.

**5. 쓰지 않는 컬럼 (REQ-325).** 데이터셋의 `ignore_undeclared_columns`(기본 거짓)를 켜면 선언하지 않은 컬럼을 `undeclared_column`으로 보고하지 않는다. 선언한 컬럼이 사라진 것은 계속 `shape_changed`로 보고한다.

**6. 소스 정의 초안 (REQ-326).** `ingest inspect -emit-source`는 파일의 프로파일에서 `source.json` 초안을 표준 출력에 낸다(파일 하나는 데이터셋 하나, 엑셀은 보이는 시트마다 하나). 시트 이름, 머리글 행(머리글 행이 여럿이면 그 수), 컬럼(영문과 숫자의 머리글은 소문자와 밑줄 이름으로, 그렇지 않으면 머리글 그대로를 이름으로 하고 원래 머리글을 `paths`에 둔다), 값 종류와 예시 값을 담은 컬럼의 `note`(정의의 읽기에는 영향이 없는 선택 필드)를 쓴다. 숨긴 시트와 안내는 표준 오류에 `note:` 줄로 낸다. 자연 키는 파일에서 알 수 없으므로 추측하지 않고 플레이스홀더 문구(`NaturalKeyPlaceholder`)를 둔다. 그대로 `ingest run`에 넣으면 정의 검사가 "natural_key가 가진 컬럼이 없다"는 오류와 데이터셋의 컬럼 목록을 알리고, 안내는 비어 있지 않고 서로 다른 컬럼을 후보로 알린다. 비워 두면 안 되는 이유는 비어 있는 자연 키의 오류와 같은 문장으로 컬럼 목록을 알리기 때문이다.

**7. 오류 메시지 (REQ-327).** 정의 JSON의 `unknown field` 오류는 그 필드가 있는 위치(`refinements[0].ops[1]`)와 거기서 허용되는 필드를 싣는다(구조체의 태그를 읽어 만든다). `missing_natural_key`는 비어 있는 컬럼 이름을 말한다(`Unreadable.Reason`의 코드는 그대로 두고 `Column`을 더했다). 서버에 묻지 않는 명령(`ingest`, `demo`, `local` 등)을 `--endpoint`, `--token`, `--config`, `--json` 뒤에 쓰면 그 명령은 서버에 접속하지 않아 그 플래그를 받지 않는다고 말한다.

**8. 표 출력 (REQ-328).** `health`는 live, ready, started마다 `ok (200)` 같은 줄을 낸다. `types get`은 식별 키(`1. strong: link located_in + property name`)와 속성(`total_u (integer, required)`)을 줄로 낸다. 값이 단순한 객체는 `key=value`로 보인다. `--json` 출력은 바꾸지 않는다. `types get`의 표 출력은 유효 속성 전체(자기 것, 조상, 특성에서 온 것)를 이름, 값 종류, 열거 값과 함께 보이고(`include=effective_properties`를 스스로 요청한다), 식별 키를 속성 이름으로 보이며 링크가 낀 키에는 "not usable in file ingestion"을 밝힌다(식별 키 오류 메시지도 같다). `praxium catalog`의 목록에 `layers`를 넣는다. 문제 메시지의 코드 접두어는 한 번만 나온다.

**9. 도움말 (REQ-329).** `ingest`와 `ingest run --help`가 형식 안내서 docs/guides/ingest-definitions.md를 가리키고, `ingest run --help`가 요약 줄의 항목(`not_read`, `skipped_blank_row`, `skipped_hidden_row`, `skipped_after_end`, 문제의 종류)을 설명한다. `ingest inspect --help`는 `-delimiter`, `-emit-source`, 인코딩 조건을 설명한다.

**10. 안내서를 실행 파일과 함께 (REQ-330).** 릴리스 압축 파일에는 문서가 없고 설치한 곳에서 GitHub을 볼 수 없는 사용자가 있으므로, `ingest init`이 예제 폴더에 `ingest-definitions.md`를 함께 쓴다. 원본은 docs/guides/ingest-definitions.md 하나이고 실행 파일에 넣는 사본은 `embed`로 담는다(`go:embed`는 상위 폴더를 가리킬 수 없어 `backend/internal/ingestcheck/guide/`에 사본을 둔다). `ingest init`이 쓴 파일이 원본과 한 바이트도 다르지 않은지 테스트가 확인하므로 어긋나면 실패한다. 원본을 고치면 사본도 같은 변경에서 복사한다.

## 이유

- 자기 파일을 처음 넣는 사람은 정의를 하나씩 고치며 실행을 되풀이한다. 한 번의 실행이 무엇이 틀렸고 무엇이 가능한지 말해 주면 되풀이 횟수가 줄고, 안내서를 찾으러 가는 일이 줄어든다.
- 모든 목록과 키 설명은 모델이나 정의의 구조에서 만든다. 문서에 쓴 목록은 모델이 바뀌면 틀리지만 코드가 만든 목록은 바뀐 대로 나온다.
- 인코딩은 파일을 읽기 전에 거절해야 한다. 깨진 글자로 읽은 데이터는 컬럼 이름이 맞지 않는 한 줄의 문제로만 나타나 원인이 숨는다.

## 대안과 기각 사유

- **CP949를 지원하기 위해 `golang.org/x/text`를 추가하기.** 새 서드파티 의존성이고 폐쇄망 배포의 검증 비용이 든다. 사용자는 엑셀에서 UTF-8로 저장하거나 `.xlsx`를 쓰면 된다.
- **인코딩을 자동으로 감지해 읽기.** 오판하면 조용히 깨진 데이터가 들어간다.
- **`encoding` 필드를 소스 정의에 두기.** 지원하는 값이 UTF-8뿐인 필드는 오해만 만든다. 지원하는 인코딩이 늘 때 정한다.
- **자연 키를 첫 번째 유일한 컬럼으로 추측해 초안에 채우기.** 맞을 때도 많지만 틀리면 조용히 틀린 키로 수집이 된다. 초안이 그대로 수집되지 않고 무엇을 고를지 알리는 편이 원칙에 맞다.
- **초안에 값 종류를 `type` 같은 새 필드로 넣기.** 파이프라인이 읽지 않는 필드를 값의 종류처럼 보이게 하면 정의의 의미가 흐려진다. 사람이 읽는 `note`로 한정했다.
- **오류의 계약(`Problem`)에 가능한 값을 구조로 넣기.** 이 결정은 메시지 문장만 바꾼다. 구조화한 힌트는 웹 UI가 쓰기로 정할 때 계약과 함께 따로 결정한다.
- **잠긴 `testdata`를 그대로 예제에 쓰며 `amends_tests`로 바꾸기.** 테스트 데이터를 예제의 요구로 흔들게 된다. 사본을 두는 편이 싸다.

## 결과

- 알려진 한계. 안내서의 사본은 원본을 고칠 때마다 손으로 복사한다(테스트가 어긋남을 잡는다).
- 알려진 한계. 한국어 Windows의 엑셀이 "CSV(쉼표로 분리)"로 저장한 파일은 CP949이므로 처음에는 멈춘다. 오류가 방법을 말한다.
- 알려진 한계. 이 결정은 걸러낸 행(소계, 합계)의 수식 셀 문제(`formula_without_value`), 계산하는 속성(volatile)의 필터, `ingest init --json`, 대시 표기 통일, `--json --limit`의 다음 페이지 정보를 다루지 않는다.
- 새 서드파티 의존성이 없다. `harness/config`의 규칙, 한도, 허용 목록, 잠긴 테스트는 바꾸지 않았다. 바뀐 보호 경로는 요구사항 표(REQ-320~329, 331 추가), ADR-0041의 `amended_by`, 새 테스트의 잠금(`test-lock.json`)이다.
