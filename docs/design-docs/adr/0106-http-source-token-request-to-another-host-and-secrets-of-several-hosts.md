---
title: ADR-0106 HTTP 소스의 다른 호스트 인증 서버 — 토큰 주소, 자격 증명과 토큰이 가는 곳, 여러 호스트를 허용하는 시크릿
kind: decision
status: active
enforced_by:
  - TLOCK001
  - GOCOV001
  - backend/internal/ingest/hxc_defs_test.go
  - backend/internal/httpsource/hxc_cross_test.go
  - backend/internal/httpsource/hxc_egress_test.go
touches_protected:
  - docs/product-specs/requirements-ingestion.md
  - docs/design-docs/adr/0092-http-source-adapter-adapter-httpsource.md
  - docs/design-docs/adr/0100-http-source-extension-baseline-basic-token-requests-and-where-secrets-go.md
  - harness/config/test-lock.json
updates_docs:
  - docs/design-docs/sources-and-adapters.md
  - docs/design-docs/secrets-and-encryption.md
  - docs/design-docs/source-editing.md
  - docs/product-specs/requirements-ingestion.md
amends:
  - adr: "0092"
    what: 토큰 주소가 `base_url`과 같은 호스트여야 한다는 제한(다른 출처의 토큰 서버를 허용하되 자격 증명은 토큰 서버로만, 토큰은 API로만 가게 한다)
  - adr: "0100"
    what: 뒤의 PR로 미뤘던 E2(다른 호스트의 인증 서버)의 구현과 그 세부 결정
amends_tests:
  - go:backend/internal/httpsource::TestTheRequestIsMadeInOnePlaceWithTheTwoMethodsAndNoOtherNetworkCodeIsImported
  - go:backend/internal/httpsource::TestTheValuesOfASecretAreTakenOutInOneFileAndNothingIsLoggedOrPrinted
test_change_reason: 두 테스트는 패키지의 모든 .go 파일을 go:embed로 읽어 제품 코드의 글자를 검사하므로 제품 코드가 바뀌면 지문이 바뀐다. 단언은 한 글자도 바뀌지 않았고, 새 파일(tokenask.go)과 바뀐 파일이 같은 규칙(값을 꺼내는 곳은 auth.go 한 곳, 요청을 만드는 곳은 request.go 한 곳, 금지한 글자와 import 없음)을 지키는 것을 새 지문으로 다시 기록한다. 같은 호스트만 허용하던 단언을 가진 잠긴 테스트는 없었다.
decision_hash: bf82ddcb9b70491f
---
# ADR-0106 HTTP 소스의 다른 호스트 인증 서버

## 맥락

ADR-0100이 E2(다른 호스트의 인증 서버에서 받는 토큰)를 결정만 하고 미뤘다. OAuth2의 `client_credentials`는 인증 서버가 API와 다른 호스트인 경우가 대부분이라 지금의 "토큰 주소는 `base_url`과 같은 호스트" 제한으로는 붙일 수 없다. 다른 호스트를 허용하면 시크릿이 두 호스트에 가므로, 무엇이 어디로 가는지를 구조로 묶어야 한다.

## 결정

1. **정의.** `auth.request.path`(`base_url` 아래)나 `auth.request.url`(절대 주소) 가운데 하나를 쓴다. 출처는 스킴, 호스트, 포트(기본 포트를 채움)이고, `url`이 `base_url`과 다르거나 읽을 수 없으면 다른 출처다. 같은 출처의 `url`은 `path`와 같게 다룬다. 새 필드는 `auth.request.url`, `auth.request.ca_pem`(다른 출처의 `url`에서만, 그 토큰 서버만 믿는 인증 기관), `auth.request.headers`(토큰 요청만의 글자 헤더), `auth.expires_in_path`다. 새 정의 오류 코드는 만들지 않고 기존 코드(`ecAdTokenReq`, `ecAdTokenUse`, `ecAdHeaderFrom`, `ecAdElsewhere`, `ecAdCAPEM`)를 쓴다.
2. **자격 증명은 토큰 호스트로만, 토큰은 API로만.** 다른 출처이면 API 쪽 요청의 `{{credential.*}}`와 `credential.*`를 읽는 `auth.headers`는 정의 오류다. 실행 때도 어댑터가 한 번 더 막는다(`secret_unusable`, 요청 0건). `connection.headers`는 토큰 서버로 보내지 않는다. 토큰 요청 안의 `{{auth.token}}`은 요청을 보내지 않는다. 토큰 서버로 가는 토큰은 없다(401 뒤 다시 받을 때도 옛 토큰을 싣지 않는다).
3. **시크릿의 범위.** 시크릿은 `allowed_hosts`에 호스트와 포트를 여럿 허용할 수 있고 정하는 사람은 운영자나 관리자다. 정의 편집자는 넓힐 수 없다. 어댑터는 시크릿을 API 주소로 한 번, 토큰 주소로 한 번 연다(`secrets.Open`의 한 곳 검사가 그대로 판정한다). 토큰 주소가 목록에 없으면 두 서버에 요청이 0건이다(점검과 미리 보기도 같다).
4. **토큰의 수명.** `expires_in_path`가 가리키는 수명(숫자나 숫자만 든 글)을 주입된 시계로 30초에서 24시간 사이로 맞추고, 끝나기 10분의 1(최대 30초) 전에 다시 받는다. 수명이 없거나 숫자가 아니면 소스가 거절할 때까지 쓴다. 401 뒤의 다시 받기는 요청마다 한 번이고 한 읽기에서 3번까지이며(검토의 R4), 넘으면 `auth_failed`다. 토큰은 16 KiB까지이고 헤더가 될 수 없는 글자가 있으면 `response_format`이다. 토큰 요청은 `max_pages`가 세는 요청에 들어간다. 그래서 토큰 요청이 있는 소스는 데이터 쪽이 한 페이지 줄어든다.
5. **egress.** 토큰 요청은 다른 요청과 같은 egress 검사를 모두 지난다. API와 토큰 서버는 각자의 인증 기관(`connection.ca_pem`, `auth.request.ca_pem`)으로 만든 클라이언트를 쓰므로 호스트마다의 설정이 섞이지 않고 같은 IP로 풀리는 두 이름도 마찬가지다(R3). 사건과 교환 기록의 호스트와 포트는 요청이 간 곳이다(`Exchange`에 `host`, `port`를 더했다). 점검은 다른 출처의 토큰 요청을 `token_request` 항목으로 따로 알린다.
6. **권한.** `request.url`, `request.ca_pem`, `request.headers`, `expires_in_path`의 변경은 접속 변경이다(`ClassifyChange`의 분류표).
7. **판단을 적어 둔 곳(설계 책임자가 보는 것).** (가) 토큰 서버의 `ca_pem`을 E2의 필드로 더했다(고정은 B1의 몫이고 인증 기관만이다). (나) 토큰 요청을 `max_pages`에 세어 토큰 요청이 있는 소스의 데이터 페이지가 하나 준다. (다) 수명이 숫자가 아니면 오류가 아니라 무시한다. (라) 같은 출처의 `url`은 절대 경로로 다룬다. (마) `ecAdTokenReq`의 설명 글에 주소와 인증 기관과 헤더를 더했다.

## 범위 밖

E5·E6(B1), E4·E8(A2), E7(C)은 이 PR이 아니다.

## 호환의 변화

- `request.path`만 쓰던 정의는 그대로 된다. `max_pages`가 작은 `token_request` 소스는 데이터 페이지가 하나 준다.
- 한 읽기에서 401이 4번을 넘는 소스는 이제 `auth_failed`로 끝난다.

## 확인

- `ingest`의 `hxc_defs_test.go`(주소의 형태, 출처 비교, 정의 규칙, 헤더, 수명, 분류)와 `httpsource`의 `hxc_cross_test.go`(두 가짜 출처, 폼과 Basic과 JSON 키, 시크릿의 두 번 열기, 401과 예산, 수명, 길이, 사건의 호스트, 점검)와 `hxc_egress_test.go`(진짜 egress 클라이언트에서 토큰 호스트의 정책 거절, 평문 거절, 쓸 수 없는 인증 기관. 호스트마다의 인증 기관 신뢰와 리디렉션 거절은 egress 유닛의 기존 테스트가 맡는다).
- 잠긴 테스트: 위 둘의 지문만 바뀐다.
