---
title: ADR-0092 HTTP API 소스를 읽는 어댑터 — 새 유닛 `adapter.httpsource`, 연결은 `adapter.egress`의 `Client`만 쓴다
kind: decision
status: active
enforced_by:
  - GOARCH010
  - GOARCH021
  - GOCOV001
  - TLOCK001
  - backend/internal/httpsource/auth_test.go
  - backend/internal/httpsource/paging_test.go
  - backend/internal/httpsource/limits_test.go
  - backend/internal/httpsource/errors_test.go
  - backend/internal/httpsource/leak_test.go
  - backend/internal/httpsource/check_test.go
  - backend/internal/httpsource/more_test.go
touches_protected:
  - docs/product-specs/requirements-ingestion.md
  - docs/product-specs/requirements-access.md
  - harness/config/architecture.yaml
  - harness/config/coverage-floor.json
  - harness/config/test-lock.json
updates_docs:
  - ARCHITECTURE.md
  - docs/design-docs/sources-and-adapters.md
  - docs/product-specs/requirements-ingestion.md
  - docs/product-specs/requirements-access.md
amends: []
amends_tests: []
decision_hash: bfb855537ef4cc22
amended_by:
  - adr: "0097"
    what: 결정 5·6·8·9의 같은 응답 확인, 읽는 방식, 토큰 만들기와 남는 위험
---
# ADR-0092 HTTP API 소스를 읽는 어댑터

## 맥락

[ADR-0090](0090-http-postgres-source-definitions-and-ingest-adapter-parts.md)이 HTTP 소스 정의의 형식과 어댑터가 쓸 부품(`RowBuilder`, `PageWriter`, `SourceError`, 가리기 함수, `Observer`, `Previewer`)을 만들었고, [ADR-0091](0091-egress-defence-in-one-unit-adapter-egress.md)이 나가는 연결을 한 곳(`adapter.egress`)에 모았다. 둘 사이를 잇는 어댑터가 아직 없다. ADR-0091은 "뒤로 미룬 일"로 304와 예상 못 한 상태 코드의 처리, 메서드 제한, `ca_pem`의 엄격한 검사, `Client`를 읽기마다 새로 만드는 일을 이 어댑터에 넘겼다.

## 결정

1. **새 유닛 `adapter.httpsource`**(`backend/internal/httpsource`, `may_import: [core.kernel, core.ingest, core.secrets, adapter.egress]`). `core.ingest`의 `PageReader`(`ReadPages`, `Check`)와 선택 인터페이스 `Observer`, `Previewer`를 구현한다. `net`, `net/http`, `crypto/tls`를 import하지 않는다(구조 검사가 막고, 허용 목록은 넓히지 않았다).
2. **연결은 좁은 인터페이스로 받는다.** 어댑터는 `Doer`(`Do(ctx, egress.Request)`와 `CloseIdleConnections()`)와 이것을 만드는 `Connector`를 받는다. 조립부는 `ViaDialer(*egress.Dialer)`로 진짜 `egress.Client`를 만들어 넣고 테스트는 가짜를 넣는다. **읽기 한 번마다 새 `Doer`를 만들고 끝에(오류와 취소에도) 닫는다**(ADR-0091 남는 위험 6).
3. **요청은 만드는 곳이 한 곳(`request.go`)이다.** 메서드는 `GET`과 `POST`뿐이고 POST는 정의가 `post_reads`로 밝혔을 때와 토큰 발급에만 쓴다. `Accept-Encoding`은 어댑터가 설정하지 않는다(압축을 요청하지 않는다). 다음 주소는 `egress.ResolveSameOrigin`을 지나야 한다. `ca_pem`은 `egress.NewClient`가 읽고 인증서가 아니면 TLS 설정 오류(`tls_rejected`)로 끝낸다.
4. **인증 다섯**: `none`, `bearer`, `basic`, `header`, `token_request`. 시크릿은 여는 포트(`secrets.Opener`)만 받는다. 값을 꺼내는 `Reveal()`은 `auth.go`의 한 곳뿐이고(스캔 테스트) 꺼낸 값은 `[REDACTED]`로만 찍히는 감싼 타입에 두며 가리기 목록에 넣는다. `token_request`의 토큰은 한 읽기 안에서만 기억하고 401이면 한 번 다시 받는다. 토큰 발급 응답의 본문은 오류에 넣지 않는다.
5. **페이지 넘김 여섯**: `none`, `page`, `offset`, `cursor`, `next_url`, `link_header`(RFC 8288의 `Link`를 읽는다). 끝은 종류마다 ADR-0090의 표대로 확인한다. 완전성은 **끝을 확인했을 때만 완전**이다. `max_pages`에 닿음, 같은 응답 본문이 되풀이됨(해시로 알아본다), `total_path`와 받은 수의 불일치는 불완전한 수집이다. 204는 빈 페이지다. 응답은 JSON만 받는다(`Content-Type`이 없으면 받고, 있는데 JSON이 아니면 `response_format`).
6. **한도와 재시도.** 응답 하나는 **압축을 푼 뒤** `max_response_bytes`까지 읽는다(Go의 전송 계층이 gzip을 알아서 풀므로 읽는 쪽에서 센다). 넘으면 **불완전한 수집이 아니라 설정 오류**(`response_format`)다: 같은 요청은 같은 크기로 다시 오므로 일부만 받고 완전하다고 하면 안 되고 정의를 고쳐야 한다. 요청마다 시간 제한(`timeout_seconds`)은 `egress.ClientOptions.Timeout`으로 건다. 요청 속도는 간격을 재는 방식(GCRA, 몰아 보내기 없음)으로 지키고 시계와 잠을 주입받는다(`time.Now`와 진짜 잠은 테스트에 없다). 재시도는 **GET의** 429, 408, 5xx와 끊긴 연결·시간 초과(`egress`의 `connect`, `timeout`)에서 세 번까지다. `Retry-After`(초나 날짜)가 있으면 그만큼, 없으면 `1초 × 2^n`에 25% 이내의 흩뜨림을 더해 `max_retry_wait_seconds`로 자른다. **`Retry-After`가 그 한도보다 길면 줄여서 기다리지 않고 일시적 오류(`rate_limited`나 `source_busy`)로 끝낸다.** 401과 403과 그 밖의 4xx는 다시 보내지 않는다. 304와 3xx는 `egress`가 리디렉션 오류로 접으므로 설정 오류(`request_rejected`)다. POST는 다시 보내지 않는다.
7. **오류는 닫힌 종류로 바꾼다**(`SourceError`). `egress`의 종류: `policy`는 `policy_denied`, `resolve`는 `name_not_found`, `tls`는 `tls_rejected`, `redirect`는 `request_rejected`(이상 설정), `connect`는 `connection_lost`, `timeout`은 `timeout`(이상 일시적). 상태 401은 `auth_failed`, 403은 `permission_denied`, 429는 `rate_limited`, 5xx는 `source_busy`. 글에는 주소, 질의, 응답 본문, 시크릿이 없고 밑의 `*egress.Error`는 `errors.As`로 운영자만 닿는다. 호출자의 취소와 기한은 소스의 장애로 세지 않고 `context` 오류 그대로 낸다.
8. **`Check`** 는 `CheckResult.Steps`를 채운다: `secret`(시크릿 열기), `connect`(접속과 TLS), `sign_in`(`token_request`일 때), `first_response`(첫 응답의 상태와 형식), `records_path`(그 위치에 레코드가 있는지). 첫 응답 하나만 읽는다. `Preview`는 `MaxRows`를 `MaxPages` 페이지에 나눠 읽고 `MaxRows`로 자르며 시크릿 값과 같은 글은 가린다. 교환(`Exchange`)은 50개까지 모은다.
9. **이어 읽기 토큰**은 JSON이고 **다음 요청을 다시 만들 위치만** 담는다(밑의 표). 시크릿, 서버가 준 주소, 질의 값은 담지 않는다. 정의에서 읽는 위치를 바꾸는 필드(출처, 데이터셋 id, 요청, `records_path`, 페이지 넘김의 `max_pages`를 뺀 설정)의 지문을 함께 담아, 정의가 바뀐 뒤의 토큰이나 모르는 토큰은 **무시하고 처음부터** 읽는다.
10. **`Observer`**: 요청, 재시도, 페이지, 끝의 사건을 알린다. 사건에는 호스트, 상태, 정해진 이유만 있고 시크릿과 질의가 없다. 어댑터는 로그를 직접 찍지 않는다.
11. **요구사항.** 구현한 부분은 새 번호로 나눈다: REQ-662(요청과 인증과 시크릿), REQ-663(페이지 넘김과 이어 읽기), REQ-664(한도와 재시도), REQ-665(오류, 점검, 미리 보기, 시크릿이 새지 않음). REQ-411, 412, 441, 442, 488, 491은 남은 부분이 있어 "예정"을 유지하고 본문에 나뉜 부분을 덧붙였다.

## 이어 읽기 토큰의 표

| 페이지 넘김 | 토큰에 담는 것 | 이어 읽을 때 |
|---|---|---|
| `none` | 응답에서 이미 낸 레코드 수(`s`), 끝이면 끝 표시(`e`) | 응답 하나를 다시 받아 `s`개를 건너뛴다 |
| `page` | 다음에 요청할 페이지 번호(`p`)와 `s` | 그 페이지부터 |
| `offset` | 다음 오프셋(`o`)과 `s` | 그 오프셋부터 |
| `cursor` | 다음 표지(`c`. 첫 요청이면 비어 있다)와 `s` | 그 표지부터 |
| `next_url`, `link_header` | 토큰을 내지 않는다 | 처음부터 |

공통으로 버전(`v`)과 지문(`fp`: 출처, 데이터셋, 요청, `records_path`, 페이지 넘김 설정의 해시 24자리)을 담고, 페이지가 한 응답을 다 못 담았을 때를 위해 응답 앞의 레코드 수(`b`)를 담는다. 토큰이 깨졌거나 지문이 다르면 처음부터 읽는다. `next_url`과 `link_header`에 토큰이 없는 것은 서버가 준 주소에 서명된 질의가 들 수 있어 토큰에 값을 넣지 않는다는 원칙이 그 주소를 담는 설계(a2 계획 3.1의 "다음 주소 저장")보다 앞서기 때문이다.

## 테스트와 진짜 연결

이 유닛의 테스트는 `net/http`를 쓸 수 없으므로(`restricted_stdlib`를 넓히지 않았다) 테스트 서버를 띄우지 않고 가짜 `Doer`로 모든 분기를 덮는다. 진짜 `egress.Client`와 붙는 확인은 가짜 리졸버가 거절 주소(메타데이터, `0.0.0.0`, 멀티캐스트)를 주는 경우와 `ca_pem`이 틀린 경우로 한정했다. 둘 다 아무것도 보내지 않는다. **진짜 HTTPS 서버와 붙는 통합 확인은 이 변경에 없다.** 하려면 `egress` 쪽의 테스트 도우미나 테스트 전용 유닛이 필요하고, 조립부를 이을 변경에서 정한다.

## 대안과 기각 사유

- **`*egress.Client`를 직접 받는다.** 테스트 서버가 `net/http`를 요구하고 구조 검사가 막는다. 좁은 인터페이스가 구조 검사를 넓히지 않고 가짜로 모든 분기를 덮는다.
- **크기 한도를 넘으면 받은 데까지 내고 불완전한 수집으로 한다.** JSON이 중간에 잘려 읽을 수 없고, 다시 읽어도 같은 결과다. 기각.
- **`Retry-After`를 한도로 줄여 기다린다.** 소스가 말한 시간보다 일찍 다시 보내 소스를 더 괴롭힌다. 기각.
- **`next_url`의 주소를 토큰에 담는다.** 서명된 질의가 토큰으로 새고 시간이 지나면 만료된다. 기각.
- **POST도 재시도한다.** 소스에 부작용이 있을 수 있다(REQ-411). 기각.
- **`Accept-Encoding: gzip`을 직접 달아 압축을 받는다.** 풀린 뒤의 바이트를 어댑터가 세야 하는 점은 같고 전송 계층의 자동 처리를 끄게 된다. 달지 않는다.

## 남는 위험

1. **시크릿이 메모리에 복사된다.** `Reveal()`로 꺼낸 값은 요청 헤더와 본문 문자열, 가리기 목록에 있고 Go의 문자열은 지울 수 없다. 읽기가 끝나면 참조를 버리지만 그 전의 힙 덤프에는 보인다. 로그와 오류와 사건에는 나가지 않는다(테스트로 고정).
2. **한도가 없는 것.** 데이터셋 전체의 바이트와 실행 전체의 시간은 정의에 필드가 없어 한도가 없다. 한 읽기는 `max_pages`, 응답당 바이트, 요청 시간으로만 묶인다. 위의 곱(1,000쪽 × 16 MiB)은 크다.
3. **`egress`가 3xx를 모두 오류로 접어** 304와 다른 3xx를 가를 수 없다. 조건부 요청(`If-None-Match`)은 쓰지 않으므로 지금은 문제가 없다.
4. **압축.** 어댑터는 압축을 요청하지 않지만 Go의 전송 계층은 서버가 보내면 gzip을 풀므로 한도는 푼 뒤의 바이트에 건다. 풀기 전의 폭탄은 이 한도가 읽는 만큼만 푼다.
5. **재시도의 시간.** `Retry-After`의 합은 최대 3 × `max_retry_wait_seconds`다. 실행 전체의 시간 한도가 없어서 이것이 실행을 길게 잡을 수 있다.
6. **같은 응답의 되풀이를 해시로 알아본다.** 정말 같은 두 페이지가 연달아 오는 소스는 불완전한 수집으로 보인다(드문 경우로 보았다).
7. **진짜 서버와의 통합 확인이 없다.** 위 "테스트와 진짜 연결"을 보라.
8. **조립부에 이어져 있지 않다.** 이 유닛을 부르는 곳이 없고 `Connector`와 시크릿 여는 포트를 넣는 일, 서버 플래그는 이어지는 변경의 몫이다.

## 결과

- 새 유닛, 커버리지 하한의 새 키(도구로), 새 테스트의 잠금이 더해진다. 잠긴 테스트와 `egress`, `ingest`의 제품 코드는 바꾸지 않았다.
- 다음 변경은 조립부에서 `ViaDialer`와 시크릿 여는 포트를 넣고 어댑터를 "종류 이름 → 여는 함수"로 등록한다.
