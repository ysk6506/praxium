---
title: ADR-0110 우회 검사의 틈 두 개 닫기 — egress 허용 import 목록과 pgsource의 SSLMode·pq.Config 규칙
kind: decision
status: active
enforced_by:
  - backend/internal/egress/hxb_scan_test.go
  - backend/internal/pgsource/hxb_scan_test.go
touches_protected:
  - docs/design-docs/adr/0105-http-source-extension-plaintext-ranges-and-certificate-pins.md
  - harness/config/test-lock.json
updates_docs: []
updates_docs_reason: 검사 규칙만 더해지고 제품의 동작과 문서가 말하는 범위(TLS 설정은 egress 한 곳에서 만든다)는 바뀌지 않는다.
amends:
  - adr: "0105"
    what: 우회 검사에 허용 import 목록(egress)과 SSLMode·pq.Config 규칙(pgsource)을 더함
amends_tests:
  - go:backend/internal/egress::TestHxbTheOnlyWayToSkipTheVerificationIsTheConfigOfThePins
  - go:backend/internal/egress::TestHxbTheScanOfTheSettingsFailsForTheWaysAround
  - go:backend/internal/egress::TestHxbTheScanOfTheSettingsFailsWhenAPlaceIsGone
  - go:backend/internal/egress::TestTheCodeHasNoWayAroundTheDefence
  - go:backend/internal/egress::TestTheDialerConnectsToTheLiteralAddressItJudged
  - go:backend/internal/pgsource::TestPgsHxbTheOnlyWayToTheTLSSettingsIsTheRegistrationOfEgress
  - go:backend/internal/pgsource::TestPgsHxbTheScanFailsForTheWaysAround
  - go:backend/internal/pgsource::TestPgsStatementsNameTheSchemaOfTheirFunctions
  - go:backend/internal/pgsource::TestPgsTheCodeHasNoWayAroundTheDefence
test_change_reason: 우회 검사를 더 엄격하게(사례와 규칙을 더함, 옛 단언 그대로). 단언은 한 글자도 바뀌지 않았고, 바뀐 것은 검사의 허용 목록, 규칙, 자기 테스트 표뿐이며 그 결과 지문이 바뀐 테스트만 적는다.
decision_hash: 49a7497104f22c15
---
# ADR-0110 우회 검사의 틈 두 개 닫기

## 맥락

ADR-0105는 TLS 확인을 끄는 길을 egress의 `pin.go` 한 곳으로 좁히고, 그 밖의 우회를 테스트가 소스 글을 읽어 막도록 했다. 독립 검토가 검사를 통과하면서도 확인을 끄는 두 꼴을 찾았다.

- **N1**: egress 제품 코드가 `encoding/json` 같은 디코더로 `tls.Config`의 필드를 이름으로 채우면 `InsecureSkipVerify`가 켜진다. 기존 검사는 필드 이름이 글자로 쓰이지 않으면 보지 못한다.
- **N2**: pgsource에서 `(cfg.SSLMode) = pq.SSLMode("re" + "quire")`처럼 괄호로 감싼 대입, `set(&cfg.SSLMode, "")`처럼 주소로 넘기는 꼴, `SSLMode`를 적지 않은 `pq.Config{…}` 리터럴이 기존 검사를 지났다.

## 결정

### N1. egress의 import 목록

1. `backend/internal/egress/hxb_scan_test.go`의 `hxbViolations`는 패키지의 제품 파일(`_test.go`가 아닌 것)이 가져오는 import를 `hxbAllowedImports` 목록과 대조한다. 목록은 지금 패키지가 쓰는 import 그대로이고, 목록 밖의 import는 위반이다.
2. 목록에 없는 것은 모두 막는다. 그중 `encoding/json`, `encoding/gob`, `encoding/xml`, `reflect`, `unsafe`, `runtime/debug`는 필드를 이름으로 채울 수 있는 길이라 특히 적는다.

### N2. pgsource의 SSLMode와 pq.Config

3. `backend/internal/pgsource/hxb_scan_test.go`의 `pgsHxbViolations`는 다음을 본다.
   - `SSLMode` 필드에 대입하는 것은, 괄호와 `*`를 벗긴 왼쪽이 정확히 `cfg.SSLMode`이고 오른쪽이 `pq.SSLMode("pqgo-" + key)`일 때만 허용한다(`useTLS`의 한 곳).
   - `SSLMode`의 주소(`&…SSLMode`)는 위반이다.
   - `pq.Config{…}` 리터럴은 `connect.go`의 `open` 함수 안에 정확히 한 번만 허용한다. 그 리터럴은 `SSLMode: pq.SSLModeDisable`을 반드시 적어야 한다.
4. 설정은 지금 코드가 만드는 한 곳의 꼴로만 만든다. 그 밖의 `pq.Config` 리터럴은 위반이다.

### 자기 테스트

5. 두 검사의 자기 테스트 표(`hxbWaysAround`, `pgsHxbWaysAround`)에 새 사례를 더한다: `encoding/json`(필드를 이름으로 채우는 디코더), `encoding/gob`, `encoding/xml`, 괄호로 감싼 대입과 글자 이어 붙이기, 주소로 넘기는 꼴, 별표로 감싼 주소, `SSLMode`가 없는 리터럴, 둘째 리터럴. 각 사례는 진짜 파일 집합에 더했을 때 위반을 내야 한다. 옛 사례는 그대로 둔다.

## 범위 밖

- 제품의 동작은 바뀌지 않는다. 이 결정은 검사의 규칙만 더한다.
- `var c pq.Config`나 `new(pq.Config)`처럼 리터럴이 아닌 꼴은 이 규칙으로 잡지 않는다. 이 꼴을 막을지는 다음 검토에서 정한다.

## 호환의 변화

없음. 검사가 더 엄격해질 뿐이고, 현재 저장소는 새 규칙을 모두 지킨다.
