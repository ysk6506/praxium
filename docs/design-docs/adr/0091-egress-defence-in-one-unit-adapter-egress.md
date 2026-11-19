---
title: ADR-0091 소스가 밖으로 붙는 연결을 한 곳에서 막는다 — 새 유닛 `adapter.egress`, 이름을 스스로 풀고 검사한 IP로 연결한다
kind: decision
status: active
enforced_by:
  - GOARCH010
  - GOARCH021
  - GOCOV001
  - TLOCK001
  - backend/internal/egress/dialer_test.go
  - backend/internal/egress/policy_test.go
  - backend/internal/egress/client_test.go
  - backend/internal/egress/host_test.go
  - backend/internal/egress/scan_test.go
touches_protected:
  - docs/design-docs/adr/0018-secrets-and-encryption.md
  - docs/design-docs/adr/0058-sources-from-db-and-api-worker-ingestion-and-editing-screens.md
  - docs/product-specs/requirements-ingestion.md
  - harness/config/architecture.yaml
  - harness/config/coverage-floor.json
  - harness/config/test-lock.json
updates_docs:
  - ARCHITECTURE.md
  - docs/design-docs/sources-and-adapters.md
  - docs/design-docs/source-editing.md
  - docs/design-docs/secrets-and-encryption.md
  - docs/product-specs/requirements-ingestion.md
amends:
  - adr: "0058"
    what: 결정 9의 TLS 확인을 낮추는 정의 필드와 서버 플래그(-allow-unverified-source-tls)
  - adr: "0018"
    what: 결정 9의 소스로 가는 연결에서 TLS 확인을 끄는 예외(verify_off)
decision_hash: cd8dac3e8f57abbe
---
# ADR-0091 소스가 밖으로 붙는 연결을 한 곳에서 막는다

## 맥락

소스 어댑터(HTTP, PostgreSQL)는 서버가 소스를 등록한 사람의 말대로 바깥(사실은 서버가 닿는 모든 내부 주소)에 연결을 맺는다. 그래서 이 연결은 서버쪽 요청 위조(SSRF)와 DNS 다시 묶기(검사할 때와 연결할 때 이름이 다른 주소를 주는 공격)의 길이다. [소스와 어댑터](../sources-and-adapters.md) 8절과 ADR-0058 결정 9가 "묶기의 코드는 어댑터 안이 아니라 따로 된 한 곳"이라고 정했고, PostgreSQL 어댑터가 같은 코드를 써야 한다(REQ-487). 어댑터끼리는 import하지 못하므로(ADR-0083) 둘이 함께 쓰는 유닛이 있어야 한다. 이 유닛은 `net`과 `net/http`를 쓰므로 구조 계약(`restricted_stdlib`)의 변경이다.

연결 순간의 검사에는 `net.Dialer.Control`이 자연스럽지만 `syscall.RawConn`을 시그니처에 담아 `syscall`을 import해야 하고, 그것은 `cmd.praxium`만 할 수 있다.

## 결정

1. **새 유닛 `adapter.egress`**(`backend/internal/egress`, `may_import: [core.kernel]`). `restricted_stdlib`의 `net`과 `net/http`에 이 유닛을 더한다. `syscall`은 그대로 `cmd.praxium`만이다. 어댑터는 서로 import하지 않고 이 유닛만 import한다.
2. **방어의 핵심은 세 단계다: 이름을 스스로 한 번 풀고, 풀린 모든 주소를 검사하고, 검사한 IP 글자로 연결한다.** 이름을 다시 풀지 않는다. 풀린 주소 가운데 하나라도 거절 범위에 있으면 연결하지 않고 전체를 거절한다. IP 글자 호스트는 풀지 않고 그대로 검사한다. TLS의 서버 이름은 호스트 이름 그대로라 인증서 확인은 달라지지 않는다. 다이얼러는 `Dial`, `DialTimeout`, `DialContext`를 가져 PostgreSQL 드라이버의 `Dialer`와 `net/http`에 그대로 들어간다(드라이버의 취소 요청도 같은 다이얼러를 지난다).
3. **호스트 글자를 먼저 검사한다.** 이름은 ASCII 영문, 숫자, `-`, `.`, `_`만(IPv6 글자는 `:`도) 받고, 사용자 정보, 경로, 포트, 존, 이스케이프, 공백, 제어 문자는 거절한다. 마지막 마디가 숫자인 이름(`2130706433`, `0x7f.1`, `0177.0.0.1`, `127.1`)은 `net/netip`가 받는 점 넷의 IPv4가 아니면 거절한다. 시스템의 이름 풀이(리졸버)가 그것을 검사기가 모르는 꼴의 IPv4로 읽기 때문이다. 그래도 풀린 주소는 어떤 경로로 나왔든 같은 검사를 받는다. 끝의 점 하나와 대소문자는 정규화한다.
4. **항상 막는 범위**(정책이 무엇이든 열 수 없다): 지정되지 않은 주소(`0.0.0.0/8`, `::`), 멀티캐스트, 예약(`240.0.0.0/4`, 브로드캐스트 포함, `192.0.0.0/24`, `100::/64`), 링크 로컬(`169.254.0.0/16`, `fe80::/10`), 알려진 메타데이터 주소(`100.100.100.200`, `168.63.129.16`, `fd00:ec2::/32`), 터널 주소(`2001::/32` Teredo, `64:ff9b:1::/48`). IPv4로 매핑된 IPv6는 `Unmap` 뒤에 IPv4로 검사한다. IPv4가 든 다른 IPv6(호환 `::/96`, 번역 `::ffff:0:0:0/96`, NAT64 `64:ff9b::/96`, 6to4 `2002::/16`)는 IPv6 주소와 **속의 IPv4 주소를 둘 다** 검사한다. 주소의 존은 지우고 검사한다(`netip.Prefix.Contains`는 존이 있는 주소에 거짓이라 `fe80::1%eth0`이 지나가기 때문이다).
5. **정책**(`Policy`): 영값은 문서의 설계 그대로다 — 위의 범위만 막고 사설 주소와 루프백은 허용한다(데이터센터의 소스는 대개 사설 대역에 있다). 더해지는 것: 운영자의 거절 범위(`Deny`. 서버 플래그 `-source-deny-hosts`가 채운다), 서버 자신의 주소와 포트(`Self`. 지정되지 않은 주소면 이 기계의 모든 주소와 루프백), 호스트와 포트의 허용 목록(`Hosts`, `Ports`. 소스 하나를 자기 주소에만 묶는다), 사설과 루프백을 막는 엄격한 정책(`DenyPrivate`)과 그 예외(`AllowRanges`). `AllowRanges`는 `DenyPrivate`가 닫은 것만 열고 항상 막는 범위, `Deny`, `Self`는 열지 못한다.
6. **TLS: 확인을 끄는 길이 없다.** TLS 1.2 이상이고 인증서와 이름을 언제나 확인한다. `ca_pem`을 주면 그 인증서만 믿고(시스템 목록을 대체) 주지 않으면 시스템 목록을 쓴다. ADR-0058 결정 9의 `connection.tls_verify: false`와 `-allow-unverified-source-tls`, PostgreSQL의 `sslmode`를 낮춘 값, 시크릿 문서의 `verify_off`는 만들지 않는다. 암호화 없는 접속은 루프백이거나 정책이 `AllowInsecure`일 때만이고(`-allow-insecure-sources`) 어댑터는 `Dialer.PlaintextAllowed`로 묻는다.
7. **프록시를 쓰지 않는다.** 전송 계층의 `Proxy`는 비어 있고 환경 변수를 읽지 않는다. 시스템 인증서 목록을 바꾸는 `SSL_CERT_FILE`과 `SSL_CERT_DIR`은 프록시가 아니라 신뢰 목록이고 폐쇄망에서 흔한 방법이라 막지 않는다.
8. **리디렉션은 따르지 않는다**(3xx는 오류, `KindRedirect`). 옵션(`MaxRedirects`)을 켜도 처음 요청과 같은 출처(스킴, 호스트, 포트)의 것만 따른다. 다른 출처로의 리디렉션은 "다시 검사해 따르기"가 아니라 거절한다: 사용자 지정 헤더(`X-Api-Key` 같은 인증 헤더)가 다른 호스트로 가기 때문이다. 다음 페이지의 주소(본문, `Link` 헤더)는 `ResolveSameOrigin`이 같은 출처인지, 사용자 정보와 역슬래시가 없는지 확인한다.
9. **요청 앞의 관문**: `http`와 `https`만, 사용자 정보가 든 주소 거절, `Host` 헤더가 주소의 호스트와 다르면 거절, 호스트 글자 검사. 응답의 헤더는 1 MiB까지, 요청 시간은 기본 30초(본문까지), 다이얼은 기본 10초(이름 풀이와 연결을 합쳐서).
10. **오류에는 주소도, 질의도, 표준 라이브러리의 문장도 없다.** `*Error`는 종류(`policy`, `resolve`, `connect`, `tls`, `timeout`, `canceled`, `redirect`)와 정해진 글과 호스트 이름만 가진다. 거절한 IP는 글에 넣지 않는다(내부 이름 풀이의 답을 알아내는 길이 되기 때문이다). `Sanitize`는 `net/http`와 `crypto/tls`의 오류를 이것으로 바꾼다. 호출자가 취소한 것은 `errors.Is(err, context.Canceled)`로 알 수 있고 소스의 장애와 섞이지 않는다. `Timeout()`과 `Temporary()`를 가져 수집 코어의 점검 분류가 그대로 읽는다.
11. **이 유닛은 로그를 찍지 않는다.** 속도 제한과 재시도, 응답 크기의 한도, 시크릿의 열기는 어댑터의 일이다(이 유닛은 응답 본문을 읽지 않는다).
12. **요구사항.** 이 유닛이 구현한 부분은 새 요구사항 REQ-658로 적고 테스트가 그 번호를 인용한다. REQ-412와 REQ-487은 정의의 검사와 어댑터가 남아 "예정"을 유지하고(구현한 부분만 덮는 테스트가 인용하면 추적 검사가 거절한다), 본문에 REQ-658로 나뉜 부분과 바뀐 부분(TLS)을 덧붙인다.

## 위협과 방어

| 위협 | 방어 | 증명하는 테스트 |
|---|---|---|
| 소스의 호스트가 내부 서비스나 클라우드 메타데이터(`169.254.169.254`)를 가리킨다 | 항상 막는 범위(결정 4). 정책으로 열 수 없다 | `policy_test.go`의 두 표 |
| DNS 다시 묶기: 검사할 때는 공개 주소, 연결할 때는 내부 주소 | 한 번만 풀고 검사한 IP 글자로 연결한다. 다시 풀지 않는다 | `dialer_test.go`(첫 답은 공개, 둘째 답은 메타데이터인 가짜 리졸버), `scan_test.go`(풀이와 연결이 한 곳씩) |
| 한 이름이 공개 주소와 내부 주소를 섞어 준다 | 하나라도 막히면 전체 거절. 순서가 달라도 같다 | `dialer_test.go` |
| 10진수, 16진수, 8진수, 짧은 꼴의 IPv4 글자(`2130706433`, `0x7f.1`, `127.1`) | 호스트 글자 검사가 거절하고, 어떻게 풀려도 풀린 주소를 검사한다 | `host_test.go`, `dialer_test.go` |
| IPv6로 감싼 IPv4(`::ffff:169.254.169.254`, NAT64, 6to4, 호환, 번역) | `Unmap`과 속 주소의 검사 | `policy_test.go` |
| 존(`fe80::1%eth0`), 끝의 점, 대문자로 우회 | 존을 지우고, 끝의 점과 대소문자를 정규화한다. `%`는 호스트 글자에서 거절한다 | `host_test.go`, `policy_test.go` |
| 서버 자신의 API나 같은 기계의 서비스에 연결 | `Self`(지정되지 않은 주소면 이 기계의 모든 주소와 루프백), `Deny` | `policy_test.go`, `dialer_test.go`(진짜 루프백 리스너) |
| 리디렉션이나 다음 페이지의 주소로 다른 호스트에 간다 | 기본은 따르지 않음. 켜도 같은 출처만. 다음 주소는 `ResolveSameOrigin` | `client_test.go` |
| 중간자나 위조 인증서 | 언제나 확인. `ca_pem`이면 그것만 신뢰. 끄는 길이 없다 | `client_test.go`(다른 기관, 시스템 목록, 만료, 이름 불일치), `scan_test.go`(우회하는 글자가 코드에 없다) |
| 환경 변수의 프록시로 우회하거나 가로채인다 | `Proxy`가 비어 있다 | `client_test.go`(프록시 환경 변수를 켜고 연결), `scan_test.go` |
| `http`로 자격 증명을 평문에 보낸다 | 루프백이 아니면 거절(`AllowInsecure`만 연다) | `client_test.go` |
| 오류의 글로 시크릿, 질의, 내부 주소가 샌다 | 오류는 정해진 글. `Sanitize`가 `url.Error`와 `net.OpError`의 글을 버린다 | `errors_test.go`, `client_test.go`, `dialer_test.go` |
| 사용자 정보나 `Host` 헤더로 다른 곳을 가리킨다 | 요청 앞의 관문 | `client_test.go` |

## 이유

- **이름을 스스로 푸는 방식**은 `net`만으로 되고 DNS 다시 묶기를 구조로 막는다: 검사와 연결이 같은 값(IP 글자)을 쓰므로 둘 사이에 끼어들 틈이 없다. `Control` 훅은 `syscall` 때문에 쓸 수 없고, 쓴다 해도 이름이 연결마다 다시 풀린다.
- **유닛 하나**는 REQ-487이 "같은 다이얼러", 문서 8절이 "따로 된 한 곳"을 요구하는 것과 어댑터 사이의 import 금지가 함께 정한 모양이다.
- **TLS를 끄는 길을 없앤 것**은 이 유닛이 줄 수 있는 가장 단순한 약속이다. 끄는 길은 한 번 생기면 운영자의 편의로 계속 쓰이고, 사설 인증 기관은 `ca_pem`으로 풀린다.
- **사설과 루프백을 기본으로 허용**하는 것은 설계 문서의 결정이다(데이터센터의 소스는 사설 대역에 있다). 이 결정을 뒤집지 않고 엄격한 정책을 옵션으로 둔다.

## 대안과 기각 사유

- **`net.Dialer.Control`로 연결 순간에 검사한다.** `syscall`을 import해야 한다(구조 계약이 `cmd.praxium`에만 허용). 이름이 연결마다 다시 풀리는 것은 훅이 막아 주지만 이름을 풀고 검사하는 쪽이 같은 효과를 더 적은 규칙으로 낸다.
- **이름을 한 번 풀어 검사하고 원래 이름으로 연결한다.** DNS 다시 묶기가 그대로 통한다.
- **허용 목록만 쓴다(거절 목록 없이).** 소스마다 주소를 적게 하는 것이 가장 안전하지만 REQ-174의 `endpoints`는 첫 구현에서 거두었고, 이 유닛은 목록(`Hosts`)을 옵션으로 갖되 거절 범위를 항상 건다.
- **TLS 확인을 끄는 예외를 서버 플래그 뒤에 둔다**(ADR-0058 결정 9의 원안). 한 번 생기면 막는 장치가 플래그 하나뿐이다. 기각.
- **다른 출처로의 리디렉션을 다시 검사해 따른다.** 다이얼러가 새 호스트를 검사하더라도 요청에 실린 사용자 지정 인증 헤더가 새 호스트로 간다. 기각.
- **프록시를 명시 설정으로 받는다.** 지금 필요한 곳이 없고 검사 순서가 달라진다(프록시에는 이름을 넘기므로 풀리는 주소를 우리가 볼 수 없다). 필요가 확인되면 따로 설계한다.
- **속도 제한을 이 유닛에 둔다.** 응답과 재시도를 아는 어댑터의 일이다. 이 유닛에는 시계가 필요 없다.

## 남는 위험

검토자가 볼 곳이다.

1. **기본 정책이 사설과 루프백을 허용한다.** 소스를 등록할 수 있는 사람은 서버가 닿는 모든 사설 주소와 포트에 연결하고(HTTP는 GET, PostgreSQL은 접속) 응답의 일부를 볼 수 있다. 오류의 종류(연결 거부와 시간 초과)로 포트가 열려 있는지도 알 수 있다. 완화: 등록 권한(`edit_definitions`와 `run_sources`), `Self`와 `Deny`, 엄격한 정책, 소스마다 `Hosts`와 `Ports`. 이 결정은 설계 문서의 것이다.
2. **같은 기계의 다른 서비스**는 `Self`에 적은 서버의 API 주소 외에는 막지 못한다(예: 서버가 쓰는 PostgreSQL의 포트). 운영자가 `Deny`로 더해야 한다.
3. **메타데이터 주소의 목록은 알려진 것뿐이다.** 링크 로컬 밖(사설 대역 안)에 메타데이터를 두는 환경은 운영자가 `Deny`에 더해야 한다.
4. **NAT64의 속 주소는 표준 접두어(`64:ff9b::/96`)만 읽는다.** 사이트가 다른 접두어를 쓰면 속 주소를 읽지 못한다.
5. **이름 풀이는 시스템의 리졸버를 믿는다**(`/etc/hosts`와 내부 DNS). 내부 DNS를 제어하는 공격자가 이름을 사설 주소로 풀게 하면 기본 정책은 그것을 허용한다.
6. **연결 재사용.** 검사는 연결을 맺을 때만이다. 한 `Client` 안에서 다시 쓰는 연결은 처음에 검사한 IP다. `Client`를 실행마다 새로 만들고 끝나면 닫는 것은 어댑터의 일이다.
7. **응답 본문의 한도는 이 유닛에 없다.** 압축 폭탄과 무한한 본문은 어댑터가 읽는 쪽에서 막아야 한다(Go는 gzip을 자동으로 풀므로 풀린 뒤의 바이트에 한도를 건다).
8. **비 ASCII 호스트는 거절한다.** `net/http`는 국제화 이름을 변환해 넘기지만 PostgreSQL 드라이버는 그대로 넘기므로 어댑터가 미리 변환하지 않으면 PostgreSQL의 비 ASCII 호스트는 쓰지 못한다.
9. **Teredo 전체와 `64:ff9b:1::/48`를 거절한다.** 이것을 쓰는 합법적인 소스는 쓰지 못한다(드문 경우로 보고 열지 않았다).
10. **속도 제한이 없다.** 소스에 주는 부하는 어댑터가 막아야 한다.
11. **`Policy`를 채우는 조립부와 플래그(`-source-deny-hosts`, `-allow-insecure-sources`)는 아직 없다.** 이 유닛은 부르는 곳이 없다. 어댑터가 `Dialer`를 거치지 않고 자기 연결을 만드는 것은 `net`과 `net/http`가 이 유닛만 허용하는 구조 검사로 막힌다. 다만 `cmd.praxium`은 두 패키지를 쓸 수 있으므로 조립부가 따로 연결을 만드는 것은 검사로 막히지 않는다. 검토에서 볼 곳이다.

## 결과

- 새 유닛과 `restricted_stdlib`의 두 줄, 커버리지 하한의 새 키(도구로), 새 테스트의 잠금이 더해진다. 잠긴 테스트를 바꾸지 않는다.
- ADR-0058 결정 9와 ADR-0018 결정 9의 TLS 확인을 끄는 예외를 거두므로 두 ADR에 `amended_by`가 붙는다.
- 다음 변경(HTTP 어댑터, PostgreSQL 어댑터)은 이 유닛의 `Dialer`, `Client`, `TLSConfig`, `ResolveSameOrigin`, `Sanitize`를 쓴다.
