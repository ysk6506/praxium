---
title: ADR-0105 HTTP 소스 확장 — 평문 허용 범위(IP/CIDR 목록)와 서버 인증서 고정(SPKI 지문)
kind: decision
status: active
enforced_by:
  - TLOCK001
  - GOCOV001
  - backend/internal/egress/hxb_plain_test.go
  - backend/internal/egress/hxb_pin_test.go
  - backend/internal/egress/hxb_probe_test.go
  - backend/internal/egress/hxb_scan_test.go
  - backend/internal/ingest/hxb_defs_test.go
  - backend/internal/httpsource/hxb_check_test.go
  - backend/internal/sourcecheck/hxb_pgpin_test.go
touches_protected:
  - docs/product-specs/requirements-ingestion.md
  - docs/product-specs/requirements-operations.md
  - docs/design-docs/adr/0091-egress-defence-in-one-unit-adapter-egress.md
  - docs/design-docs/adr/0093-postgres-source-adapter-keyset-pages-and-driver-containment.md
  - harness/config/test-lock.json
  - harness/config/architecture.yaml
updates_docs:
  - docs/design-docs/sources-and-adapters.md
  - docs/design-docs/source-editing.md
  - docs/design-docs/secrets-and-encryption.md
  - docs/product-specs/requirements-ingestion.md
  - docs/product-specs/requirements-operations.md
  - ARCHITECTURE.md
amends:
  - adr: "0091"
    what: TLS 확인을 끄는 길이 없다는 결정을 인증서 고정 한 곳으로 좁혀 고친 것, 평문 허용을 AllowInsecure 하나에서 IP/CIDR 목록(PlaintextRanges)으로 넓힌 것
  - adr: "0093"
    what: PostgreSQL 소스의 TLS 설정을 egress의 등록 함수 하나로만 만들도록 한 것, 점검이 인증서 요약과 평문 안내를 돌려주는 것
amends_tests:
  - go:backend/internal/egress::TestTheCodeHasNoWayAroundTheDefence
  - go:backend/internal/pgsource::TestPgsTheCodeHasNoWayAroundTheDefence
  - go:backend/internal/ingest::TestAdpEveryCodeOfTheAdapterTableIsGivenAtItsPlace
  - go:backend/internal/egress::TestTheDialerConnectsToTheLiteralAddressItJudged
  - go:backend/internal/httpsource::TestTheRequestIsMadeInOnePlaceWithTheTwoMethodsAndNoOtherNetworkCodeIsImported
  - go:backend/internal/httpsource::TestTheValuesOfASecretAreTakenOutInOneFileAndNothingIsLoggedOrPrinted
  - go:backend/internal/pgsource::TestPgsStatementsNameTheSchemaOfTheirFunctions
  - go:backend/internal/egress::TestTLSConfigAlwaysVerifies
  - go:backend/internal/egress::TestTLSConfigTrustsOnlyTheGivenAuthority
  - go:backend/internal/egress::TestTLSConfigRefusesATextWithoutACertificateAndDoesNotEchoIt
test_change_reason: 앞의 둘은 TLS 확인을 건너뛰는 글자(InsecureSkipVerify)를 egress의 고정 설정 한 곳에만 허락하는 것으로 검사를 바꾼다. egress 쪽은 금지 글자 목록에서 빠지는 것이 InsecureSkipVerify와 VerifyConnection뿐이고 이 둘은 pin.go에 정확히 한 번씩만 있어야 하는 검사로 옮기며 KeyLogWriter와 EncryptedClientHello를 금지 목록에 더하고, 필드 허용 목록의 AST 단언은 새 파일의 테스트가 맡으므로 합치면 이전보다 엄격하거나 같고, pgsource 쪽은 *tls.Config를 쥐거나 만드는 길을 egress의 등록 함수 호출 하나만 남기고 막으므로 엄격하거나 같다. 바꾼 검사가 우회 파일을 실제로 거절하는 것은 새 테스트 TestHxbTheOnlyWayToSkipTheVerificationIsTheConfigOfThePins가 따로 확인한다. 셋째는 정의 오류 코드 source.connection.tls_pins_invalid를 더하면서 사례 표 adpCases에 사례 세 개를 더하는 내용 변경이고, 빼거나 고친 사례와 단언은 없다. 나머지 넷은 패키지의 모든 .go 파일을 go:embed로 읽어 제품 코드의 글자를 검사하므로 제품 코드(pin.go, probe.go 같은 새 파일과 바뀐 파일)가 바뀌면 지문이 바뀐다. 단언은 한 글자도 바뀌지 않았고, 바뀐 코드가 같은 규칙을 지키는 것을 새 지문으로 다시 기록한다. 마지막 셋(TestTLSConfig로 시작하는 egress의 세 테스트)은 독립 검토(L6)에 따라 egress.TLSConfig를 비공개 tlsConfig로 바꾸면서 테스트가 함수를 부르는 이름만 바뀌는 내용 변경이다. 단언은 한 글자도 바뀌지 않았고 오류 문구의 글자도 그대로이다. 이 변경으로 egress 밖에서는 *tls.Config를 손에 쥘 수 없다.
decision_hash: a122d58193ae55ce
amended_by:
  - adr: "0110"
    what: 우회 검사에 허용 import 목록(egress)과 SSLMode·pq.Config 규칙(pgsource)을 더함
---
# ADR-0105 HTTP 소스 확장 — 평문 허용 범위와 인증서 고정

## 맥락

ADR-0100이 정한 HTTP 소스 확장 가운데 E5(평문 HTTP의 허용 목록)와 E6(인증서 고정)을 구현한다. 두 가지 현실 때문이다.

- 사내망의 장비와 개발용 서버는 평문 HTTP로만 열려 있는 일이 흔하다. ADR-0091의 `AllowInsecure`는 켜면 모든 주소에 평문을 허락하는 스위치 하나뿐이라, 운영자가 "이 대역만 평문"이라고 말할 길이 없었다.
- 자체 서명 인증서나 이름이 맞지 않는 인증서를 쓰는 서버가 있다. `ca_pem`은 체인과 이름을 모두 맞춰야 해서 이런 서버에는 쓸 수 없고, 확인을 끄는 길을 열 수는 없다(ADR-0091).

## 결정

### E5. 평문 허용 범위

1. egress `Policy`에 `PlaintextRanges []netip.Prefix`를 더한다. 평문 요청(`http://`, `sslmode=disable`)은 **받은 주소를 한 번 풀고 그 모든 주소가** 이 범위 안에 있을 때만 나간다. 이름이 아니라 풀린 주소로 판단하고, 판단한 주소 그대로 접속하는 ADR-0091의 원칙은 그대로다. 빈 목록은 루프백만이다. 모든 주소를 뜻하는 특별 표기는 없다.
2. `AllowInsecure bool`은 남긴다(옛 호출과 잠긴 테스트 그대로). 모든 주소에 평문을 허락하는 것과 같다. 둘을 함께 쓰면 정책을 만들 때 오류다(어느 쪽을 뜻하는지 알 수 없다).
3. 운영자 플래그는 `-source-plaintext-ranges`와 `-source-deny-ranges`이고 IP와 CIDR만 받는다(`egress.ParseRanges`, 최대 256개, 이름이나 와일드카드는 거절). 서버 조립 코드가 아직 없어 이 PR은 파서와 `Policy` 필드까지 두고 플래그 연결은 조립이 생길 때 한다. 문서에는 플래그를 "예정"으로 적는다.
4. 점검의 결과에 "암호화되지 않음" 안내(`NotePlaintext`)를 싣는다. 결과의 종류와 상관없이 싣는다.

### E6. 인증서 고정

5. 정의의 `connection.tls.pins`에 서버 인증서 공개 키(SubjectPublicKeyInfo)의 SHA-256을 `sha256/<base64>`로 한 개에서 여덟 개까지 적는다. 같은 지문은 한 번으로 세고 정규화해 돌려준다. `ca_pem`과 함께 쓸 수 없고, 평문 연결에는 쓸 수 없다. 다른 필드(확인을 끄는 스위치 따위)는 읽지 않는다(알 수 없는 필드).
6. **확인을 건너뛰는 곳은 egress의 `pin.go` 한 곳이다.** 고정이 있으면 `tls.Config{InsecureSkipVerify: true, VerifyConnection}`을 쓰고 `VerifyConnection`이 서버가 보낸 인증서의 지문이 목록에 있는지만 본다(이어 붙이는 세션에서도 돈다). 서버는 핸드셰이크에서 그 키를 가졌음을 증명해야 하므로 인증서 사본으로는 통과할 수 없다. 체인, 이름, 만료는 보지 않는다.
7. PostgreSQL은 egress의 `RegisterPostgresTLS`로 드라이버에 설정을 등록하고, pgsource는 `*tls.Config`를 쥐지 않는다.
8. 맞지 않는 지문은 `PinMismatchError`이고 실행에서는 `tls_rejected`(설정 종류의 오류, 다시 시도하지 않는다)로 나간다.
9. **점검은 실패한 인증서를 보여 준다.** TLS 실패면 점검이 서버가 내민 인증서의 요약(주체, 발급자, 이름, 유효 기간, 지문)을 한 번 더 접속해 가져와 결과에 싣는다(핸드셰이크는 체인을 본 뒤 거절하므로 데이터는 가지 않는다. PostgreSQL은 SSLRequest 뒤에 같은 일을 한다). 고정이 있으면 만료를 무시한다는 안내(`NotePinExpiry`)를 싣는다. 요약은 선택 인터페이스(`certProber`)로 받아 `Doer`와 잠긴 가짜들을 건드리지 않는다.
10. 고정과 `ca_pem`의 변경은 `ClassifyChange` 표에서 접속 변경이다(`edit_definitions`와 `run_sources` 모두).

### 우회 검사의 변경

11. egress의 `TestTheCodeHasNoWayAroundTheDefence`에서 `InsecureSkipVerify`와 `VerifyConnection`은 "어디에도 없어야"에서 "`pin.go`에 정확히 한 줄씩"으로 바뀌고, `KeyLogWriter`와 `EncryptedClientHello`가 금지 목록에 더해진다. 새 테스트 `TestHxbTheOnlyWayToSkipTheVerificationIsTheConfigOfThePins`는 구문 트리를 보는 판정 함수 하나(`hxbViolations`: 파일 집합을 받아 위반 목록을 낸다)를 본 검사와 자기 테스트(`TestHxbTheScanOfTheSettingsFailsForTheWaysAround`)이 함께 쓴다. 규칙은 `crypto/tls`를 별명과 점 없이만 import하고, `tls.Config`라는 글을 정해진 함수 여섯에서 포인터나 리터럴의 타입으로만 쓰고(타입 선언, 별명, 변수, `new`는 위반), 설정을 만드는 곳 둘이 각각 정해진 필드만 쓰며, `tls.Config` 필드의 선택자가 대입의 왼쪽(괄호, `*`, 인덱스 포함), `++`/`--`, `&`의 피연산자, `range`의 대상에 나오면 실패하고, `pinnedConfig`와 `probeConfig`를 부르는 곳을 정해진 함수로 못박는 것이다(`pinnedConfig`는 연결의 `pins`만 받는다). 필드 이름은 `tls.Config` 타입에서 가져오고 이름으로 판정해 같은 이름의 다른 필드도 걸리므로 넓게 잡는 쪽이다. `go/types`는 쓰지 않았다. 타입을 쓸 수 있는 자리가 위 목록뿐이라 타입으로 보아도 같은 곳이 걸리고, 소스 import는 테스트을 느리게 하고 GOROOT에 기대기 때문이다. 자기 테스트은 검토가 찾은 네 꼴(import 별명, 타입 별명, 필드 포인터와 `*(&x.F)`와 `--`, 고정 값으로 `pinnedConfig`를 부르는 다른 함수)과 그 밖의 우회를 합쳐 스물여덟 가지를 진짜 파일 집합에 더해 위반이 나오는지 보고, 필요한 자리가 없어졌을 때도 실패하는지 본다.
12. pgsource의 `TestPgsTheCodeHasNoWayAroundTheDefence`는 `tls.Config`와 확인 건너뛰기에 관한 글자를 모두 막되 egress의 등록 함수 호출 하나만 허락하도록 바뀐다.
13. 같은 방식(판정 함수 하나와 자기 테스트)의 새 검사 `TestPgsHxbTheOnlyWayToTheTLSSettingsIsTheRegistrationOfEgress`가 `egress.RegisterPostgresTLS(`와 `RegisterTLSConfig`가 각각 정확히 한 번, `connect.go`의 `useTLS`에서 드라이버의 함수를 감싸지 않고 그대로 넘기는 호출 하나에만 있을 것, `reflect`·`unsafe`·`crypto/tls`를 import하지 않을 것, `SSLMode`에는 `pq.SSLModeDisable`과 `pq.SSLMode("pqgo-" + key)`만 넣을 것(`require`, `prefer`, `allow`, `verify-ca`, `verify-full`, `disable` 글자 금지), `tls.Config`의 필드 이름(`Time` 포함)을 패키지 이름이 아닌 값에 쓰지 않을 것을 본다. egress의 `TestHxbOnlyPgsourceRegistersTLSSettingsWithTheDriver`는 백엔드 전체를 훑어 이 두 이름을 쓰는 파일이 `egress/tls.go`와 `pgsource/connect.go`뿐임을 본다. `egress.TLSConfig`는 비공개 `tlsConfig`로 바뀌어 egress 밖에서 `*tls.Config`를 손에 쥐는 길이 없다(위 amends_tests의 마지막 셋).

### 독립 검토의 낮은 발견에 따른 보강

14. 범위 파서(`egress.ParseRanges`)는 호스트 비트가 남은 범위를 오류로 하고(`10.1.2.3/16`), `/0`은 `0.0.0.0/0`과 `::/0` 표기만, IPv4를 품은 IPv6 표기는 `/96` 이상만 받는다(운영자의 오타가 평문 허용 범위를 말없이 넓히지 않게).
15. 인증서 요약(`CertSummary`)은 글이나 이름 목록이 잘리면 `Truncated`로 알린다. 요약은 서버가 쓴 글이라 화면은 반드시 이스케이프해서 글로만 보인다.
16. PostgreSQL 소스의 핀 불일치(또는 핀이 있는 정의의 TLS 실패)는 "서버의 키가 정의에 고정한 것이 아니다"로 알리고, 체인과 이름과 만료가 틀렸다는 안내를 하지 않는다.

17. **구조 설정의 변경(`harness/config/architecture.yaml`)**: HTTP 어댑터를 진짜 TLS 서버 둘(API와 토큰 서버, 각자의 인증 기관)에 공개 API로만 읽어 보는 테스트은 `net`, `net/http`, `crypto/tls`를 쓰므로, 소켓을 여는 테스트 전용 유닛 `adapter.sourcecheck`(ADR-0093)에 둔다. 그래서 `adapter.sourcecheck`의 `test_may_import`에 `adapter.httpsource`를 더하고, `restricted_stdlib`의 `net`, `net/http`, `crypto/tls`에 그 파일 하나(`httptls_test.go`)만 이름으로 더한다(유닛 전체를 열지 않는다). 인증서는 정해진 시드으로 만든다. 이 테스트은 호스트마다 인증 기관이 따로임, 토큰 서버의 리디렉션 301/302/303/307/308을 따르지 않음, 같은 주소의 다른 이름은 다른 호스트임, 다음 페이지가 토큰 서버를 가리켜도 토큰이 가지 않음, 평문 범위 안의 `http://` 토큰 서버는 읽히고 점검이 `NotePlaintext`로 알리며 범위 밖은 정책이 거절함을 본다(점검의 평문 알림은 토큰 요청의 `http://`에도 붙는다).

## 범위 밖

- 서버 조립 코드와 플래그 연결(조립이 생길 때).
- E2(다른 호스트의 인증 서버), E4(줄마다 JSON), E7(자식 데이터셋), E8(배열).
- 인증서 요약을 연결 변경 권한이 있는 사람에게만 보이는 것(점검 API가 생길 때. 아래 남는 위험).

## 호환의 변화

`tls` 필드가 없는 기존 정의는 그대로다. `AllowInsecure`의 뜻은 그대로다. 점검 결과에 `Notes`와 `Cert` 필드가 더해졌다(없으면 비어 있다). 정의 오류 코드 하나가 더해졌다.

## 남는 위험

- **처음 지문을 믿는 문제**: 점검이 보여 주는 지문을 서버 관리자에게 받은 값과 맞춰 보지 않고 고정하면 그 순간 중간자의 키를 고정하게 된다. 문서가 맞춰 보라고 안내한다.
- **만료를 보지 않는다**: 고정한 인증서는 만료돼도 연결된다. 점검의 안내로만 알린다.
- **고정을 잃으면 소스가 멈춘다**: 키를 바꾼 서버에는 고정을 먼저 더해 둬야 한다(여덟 개까지 받는 이유).
- **키가 새면 막지 못한다**: 고정한 인증서의 개인 키를 가진 쪽은 통과한다.
- **인증서 요약의 노출 범위**: 요구 사항(R17)은 요약을 접속 변경 권한이 있는 사람에게만 보이라고 한다. 점검 API가 아직 없어 강제하지 못하고, 이 ADR과 문서에 전제로 적어 둔다. API를 만드는 PR이 확인해야 한다.
- **플랫폼 전체의 평문 안내와 플래그 연결이 아직 없다**: 서버 조립 때 한다.
- **다음 PR이 볼 것**: 서버 조립 PR은 조립 코드가 `AllowInsecure`를 켜지 못하게 묶는 검사와 범위 파서 강화(결정 14)의 연결을 본다. 점검 API PR은 인증서 요약을 접속 변경 권한과 실행 권한을 함께 가진 주체에게만 보이는 것과, `ProbeCert`·`ProbePostgresCert`(정책이 허락하는 아무 호스트와 포트의 인증서를 돌려주는 공개 함수)의 감사 로그을 본다.

## 확인

`hxb` 접두어 테스트들이 평문 범위(풀린 주소 기준, 빈 목록, 겹침, 오류), 고정(일치, 불일치, 체인과 이름 무시, 세션 이어 붙임(설정에 세션 캐시가 없는 것, 그리고 캐시를 손으로 넣어도 이어 붙인 세션에서 핀 불일치가 거절되는 것), 여럿), 점검의 인증서 요약과 안내, 정의 오류와 정규화, 변경 분류, 실제 PostgreSQL 뒤의 고정 읽기, 그 읽기를 취소하는 요청이 고정한 서버로 TLS를 거쳐 가는 것, 우회 검사를 본다.
