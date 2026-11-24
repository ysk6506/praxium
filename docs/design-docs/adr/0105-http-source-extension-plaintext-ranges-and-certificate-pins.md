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
updates_docs:
  - docs/design-docs/sources-and-adapters.md
  - docs/design-docs/source-editing.md
  - docs/design-docs/secrets-and-encryption.md
  - docs/product-specs/requirements-ingestion.md
  - docs/product-specs/requirements-operations.md
amends:
  - adr: "0091"
    what: TLS 확인을 끄는 길이 없다는 결정을 인증서 고정 한 곳으로 좁혀 고친 것, 평문 허용을 AllowInsecure 하나에서 IP/CIDR 목록(PlaintextRanges)으로 넓힌 것
  - adr: "0093"
    what: PostgreSQL 소스의 TLS 설정을 egress의 등록 함수 하나로만 만들도록 한 것, 점검이 인증서 요약과 평문 안내를 돌려주는 것
amends_tests:
  - go:backend/internal/egress::TestTheCodeHasNoWayAroundTheDefence
  - go:backend/internal/pgsource::TestPgsTheCodeHasNoWayAroundTheDefence
  - go:backend/internal/ingest::TestAdpEveryCodeOfTheAdapterTableIsGivenAtItsPlace
test_change_reason: 앞의 둘은 TLS 확인을 건너뛰는 글자(InsecureSkipVerify)를 egress의 고정 설정 한 곳에만 허락하는 것으로 검사를 바꾼다. egress 쪽은 금지 글자 목록에서 빠지는 것이 InsecureSkipVerify와 VerifyConnection뿐이고 이 둘은 pin.go에 정확히 한 번씩만 있어야 하는 검사로 옮기며 KeyLogWriter와 EncryptedClientHello를 금지 목록에 더하고, 필드 허용 목록의 AST 단언은 새 파일의 테스트가 맡으므로 합치면 이전보다 엄격하거나 같고, pgsource 쪽은 *tls.Config를 쥐거나 만드는 길을 egress의 등록 함수 호출 하나만 남기고 막으므로 엄격하거나 같다. 바꾼 검사가 우회 파일을 실제로 거절하는 것은 새 테스트 TestHxbTheOnlyWayToSkipTheVerificationIsTheConfigOfThePins가 따로 확인한다. 셋째는 정의 오류 코드 source.connection.tls_pins_invalid를 더하면서 사례 표 adpCases에 사례 세 개를 더하는 내용 변경이고, 빼거나 고친 사례와 단언은 없다.
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

11. egress의 `TestTheCodeHasNoWayAroundTheDefence`에서 `InsecureSkipVerify`와 `VerifyConnection`은 "어디에도 없어야"에서 "`pin.go`에 정확히 한 줄씩"으로 바뀌고, `KeyLogWriter`와 `EncryptedClientHello`가 금지 목록에 더해진다. 새 테스트 `TestHxbTheOnlyWayToSkipTheVerificationIsTheConfigOfThePins`는 구문 트리에서 `tls.Config`의 필드를 허용 목록으로 검사하고(`KeyLogWriter`, `Time`, 암호화된 ClientHello 따위는 허용하지 않는다) 우회 파일 사례를 실제로 거절하는 것을 보인다.
12. pgsource의 `TestPgsTheCodeHasNoWayAroundTheDefence`는 `tls.Config`와 확인 건너뛰기에 관한 글자를 모두 막되 egress의 등록 함수 호출 하나만 허락하도록 바뀐다.

## 범위 밖

- 서버 조립 코드와 플래그 연결(조립이 생길 때).
- E2(다른 호스트의 인증 서버), E4(줄마다 JSON), E7(자식 데이터셋), E8(배열).
- 인증서 요약을 연결 변경 권한이 있는 사람에게만 보이는 것(점검 API가 생길 때. 아래 남는 위험).

## 호환의 변화

`tls` 필드가 없는 기존 정의는 그대로다. `AllowInsecure`의 뜻은 그대로다. 점검 결과에 `Notes`와 `Cert` 필드가 더해졌다(없으면 비어 있다). 정의 오류 코드 하나가 더해졌다.

## 남는 위험

- **처음 지문을 믿는 문제**: 점검이 보여 주는 지문을 서버 관리자에게 받은 값과 맞춰 보지 않고 고정하면 그 순간 중간자의 키를 고정하게 된다. 문서가 맞춰 보라고 안내한다.
- **만료를 보지 않는다**: 고정한 인증서는 만료돼도 연결된다. 점검의 안내로만 알린다.
- **고정을 잃으면 소스가 멈춘다**: 키를 바꾼 서버에는 고정을 먼저 더해 둬야 한다(여덟 개까지 받는 까닭).
- **키가 새면 막지 못한다**: 고정한 인증서의 개인 키를 가진 쪽은 통과한다.
- **인증서 요약의 노출 범위**: 요구 사항(R17)은 요약을 접속 변경 권한이 있는 사람에게만 보이라고 한다. 점검 API가 아직 없어 강제하지 못하고, 이 ADR과 문서에 전제로 적어 둔다. API를 만드는 PR이 확인해야 한다.
- **플랫폼 전체의 평문 안내와 플래그 연결이 아직 없다**: 서버 조립 때 한다.

## 확인

`hxb` 접두어 테스트들이 평문 범위(풀린 주소 기준, 빈 목록, 겹침, 오류), 고정(일치, 불일치, 체인과 이름 무시, 세션 이어 붙임, 여럿), 점검의 인증서 요약과 안내, 정의 오류와 정규화, 변경 분류, 실제 PostgreSQL 뒤의 고정 읽기, 우회 검사를 본다.
