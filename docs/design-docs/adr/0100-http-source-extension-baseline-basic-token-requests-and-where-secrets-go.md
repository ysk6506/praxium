---
title: ADR-0100 HTTP 소스 확장의 기준 결정 — 토큰 요청의 Basic과 본문 토큰, 시크릿과 토큰이 들어가는 자리, 접속 변경의 권한 분류
kind: decision
status: active
enforced_by:
  - TLOCK001
  - GOCOV001
  - backend/internal/ingest/hxa_defs_test.go
  - backend/internal/httpsource/hxa_places_test.go
touches_protected:
  - docs/product-specs/requirements-ingestion.md
  - docs/design-docs/adr/0090-http-postgres-source-definitions-and-ingest-adapter-parts.md
  - docs/design-docs/adr/0092-http-source-adapter-adapter-httpsource.md
  - harness/config/coverage-floor.json
  - harness/config/test-lock.json
updates_docs:
  - docs/design-docs/sources-and-adapters.md
  - docs/design-docs/source-editing.md
  - docs/product-specs/requirements-ingestion.md
amends:
  - adr: "0092"
    what: 인증의 종류와 토큰 요청(본문의 자격 증명, 토큰의 자리), 시크릿 값을 가리는 범위, 시크릿이 요청에 들어가는 자리
  - adr: "0090"
    what: 토큰 요청 본문의 규칙을 데이터 요청 본문으로 넓힌 것, 인증의 종류 여섯, 정의 변경의 권한 분류
amends_tests:
  - go:backend/internal/httpsource::TestTheRequestIsMadeInOnePlaceWithTheTwoMethodsAndNoOtherNetworkCodeIsImported
  - go:backend/internal/httpsource::TestTheValuesOfASecretAreTakenOutInOneFileAndNothingIsLoggedOrPrinted
test_change_reason: 두 테스트는 패키지의 모든 .go 파일을 go:embed로 읽어 제품 코드의 글자를 검사하므로 제품 코드가 바뀌면 지문이 바뀐다. 단언은 한 글자도 바뀌지 않았고, 새 파일(body.go)과 바뀐 파일이 같은 규칙(값을 꺼내는 곳은 auth.go 한 곳, 요청을 만드는 곳은 request.go 한 곳, 금지한 글자와 import 없음)을 지키는 것을 새 지문으로 다시 기록한다.
decision_hash: be7d865350dfd93d
---
# ADR-0100 HTTP 소스 확장의 기준 결정

## 맥락

HTTP 소스(ADR-0090, 0092, 0097)로 붙이려는 API 가운데 지금의 정의 형식으로 안 되는 것이 있다. 로그인을 Basic 인증으로 하고 응답 본문이 토큰 글자 하나인 API(세션 만들기), 키를 헤더 이름을 골라 보내거나 요청 본문의 값으로 보내는 API, 중첩된 JSON 본문으로 로그인하는 API, 다른 호스트의 인증 서버, 줄마다 JSON인 응답, 앞 요청의 결과로 다시 묻는 API, 응답 레코드 안의 배열이다. 이를 여덟 가지 확장(E1~E8)으로 설계했고 독립 검토를 거쳐 설계 책임자가 최종 결정을 내렸다. 이 ADR은 E1~E8의 결정 전체를 기준으로 적고, 이 PR(A1)은 그 가운데 E1, E3과 권한 분류만 구현한다. 뒤의 PR은 이 기록을 고치거나 채우는 짧은 ADR을 쓴다.

## 결정

### 구현하는 것(이 PR)

1. **E1. 토큰 요청의 Basic과 본문 토큰.** `token_request.request.credentials_in`은 `body`(기본)나 `basic`이다. `basic`이면 시크릿의 `username`과 `password`로 `Authorization: Basic`을 만들고 본문은 선택이다(본문 없는 POST). 사용자 이름에 `:`가 있으면 보내지 않는다(`secret_unusable`). 본문에 `{{credential.…}}`를 쓰면 정의 오류다. `token_from`은 `path`(기본)나 `body`다. `body`는 응답 본문 전체가 비어 있지 않은 JSON 글자 하나일 때만 받고 객체, 숫자, 빈 글자, null은 `response_format`이다. `token_path`와 `token_from: body`를 함께 쓰면 정의 오류이고, 둘 다 없으면 지금처럼 정의 오류다(잠긴 사례 그대로).
2. **E3. 시크릿과 토큰이 요청에 들어가는 자리.**
   - 본문: JSON 글자 값 통째로 `{{credential.<필드>}}` 또는 `{{auth.token}}`. 값의 일부로는 안 된다. 어느 깊이든 되고 값은 JSON으로 인코딩된다. 토큰 요청 본문도 데이터 요청과 같은 JSON 트리 엔진 하나가 처리한다(중첩된 본문). 토큰 요청 안에서 `{{auth.token}}`은 정의 오류다.
   - 헤더: 구조형 `auth.headers: [{name, from, prefix}]`(`from`은 `credential.<필드>`나 `token`). `connection.headers`와 데이터셋의 헤더는 지금처럼 비밀이 아닌 글자만이고, 자리표시가 든 값은 정의 오류다. 금지 헤더 목록은 `auth.headers`의 이름에도 적용하고(`Authorization`은 허용), 접근 로그에 남는 헤더(`User-Agent`, `Referer`, `Forwarded`, `Via`, `X-Request-Id`, `traceparent`, `X-Forwarded-*` 따위)는 이름으로 쓸 수 없다.
   - 질의와 경로(URL)에는 시크릿과 토큰을 넣을 수 없다(주소는 로그에 남는다).
   - `auth.kind: custom`은 자동으로 붙이는 헤더가 없고 `auth.headers`와 본문의 자리표시로만 시크릿을 쓴다. 토큰 요청의 `send`는 `header`(기본)나 `template`이다.
   - 보내기 전에 헤더로 가는 시크릿과 받은 토큰에 줄바꿈이나 제어 문자가 있으면 요청을 보내지 않고 다시 보내지도 않는다. 새 실행 오류 종류는 만들지 않고 기존 12종(`secret_unusable`, `response_format`)만 쓴다.
   - 소스가 준 값(다음 페이지 표지 따위)은 자리표시로 읽지 않고 그대로 보낸다(악의적인 서버가 시크릿을 끌어내는 길을 막는다).
3. **가리기의 범위(결정 13).** 행(데이터와 자연 키)을 바꾸는 가리기는 비밀 필드(비밀번호, 토큰, 클라이언트 비밀, API 키)만 쓰고 8바이트 이상일 때만 한다. 사용자 이름과 `client_id`는 사건, 미리 보기, 오류에서만 가린다(4바이트 이상). 새로 생긴 꼴을 가릴 집합에 넣는다: Basic의 값 그대로와 `Basic ` 접두어를 붙인 것과 폼 인코딩된 꼴, `auth.headers`의 접두어를 붙인 값, 본문 자리표시로 들어간 값.
4. **권한 분류(결정 2).** `core.ingest`의 순수 함수 `ClassifyChange(before, after)`가 두 정의를 견줘 접속 변경(`edit_definitions`와 `run_sources` 모두)인지 매핑만의 변경(`edit_definitions`)인지와 이유를 돌려준다. 접속 변경은 새 소스, 어댑터, 연결의 모든 필드(주소, 시크릿 참조, CA, 헤더, `auth` 전체, 호스트, 포트, 데이터베이스, TLS 방식), 기본보다 올린 한도(시간 제한, 초당 요청 수, 응답 크기, 재시도 대기, 페이지 수와 크기), `post_reads`와 POST 요청의 변경, 시크릿이나 토큰의 자리표시가 든 본문의 추가나 변경이다. 이 함수를 부르는 정의 저장소의 경로는 아직 없다. **정의 저장소가 저장할 때 이 함수를 부른다**(뒤의 작업). 이 PR은 함수와 테스트와 문서만 둔다. 같은 PR에서 `source-editing.md`의 점검 권한 문장을 하나로 맞췄다: 저장하지 않은 정의의 점검과 소스 탐색은 `edit_definitions`와 `run_sources` 모두(ADR-0091 결정 12).

### 뒤의 PR이 구현하는 것(여기서는 결정만)

5. **E2. 다른 호스트의 인증 서버에서 받는 토큰**(B2). 자리마다 허용하는 자리표시를 문법에 묶고, 토큰 출처가 API와 다르면 API 쪽 요청에 시크릿 자리표시를 쓸 수 없다(결정 1). 토큰 요청의 헤더는 `auth.request.headers`이고 `connection.headers`를 싣지 않는다. 토큰 주소와 인증서 고정과 CA의 변경은 접속 변경이다.
6. **E4. 줄마다 JSON인 응답**(A2). `json_lines`는 다시 받아 건너뛰지 않고 이어 읽기 토큰도 없다. 끝 표시(Content-Length를 다 받음, 청크의 정상 끝)가 있어야 완전이다. 깨진 줄은 그 데이터셋을 불완전으로 끝낸다.
7. **완전성의 셋(결정 5).** `core.ingest`의 완전성에 완전, 보인 것만, 불완전을 둔다. 보인 것만은 읽은 레코드는 반영하되 안 보인 레코드를 세지 않는다(지워짐의 유예가 오르지 않는다). E7에서 404로 건너뛴 자식, E8에서 배열이 없거나 null인 바깥 레코드(기본), `json_lines`의 끝 표시 없는 끝에 쓴다. 자식 요청의 다른 실패, 읽을 수 없는 줄, 한도 초과는 불완전이다(A2).
8. **E5. 평문 HTTP의 허용 목록**(B1). egress `Policy`의 `AllowInsecure`는 그대로 두고 `PlaintextRanges []netip.Prefix`를 더한다. 운영자 플래그는 `-source-deny-ranges`와 `-source-plaintext-ranges`이고 IP와 CIDR만 받는다. 빈 목록은 루프백만이다. 모두를 허락하는 특별 표기는 없다.
9. **E6. 인증서 고정**(B1). `ca_pem`과 함께 쓸 수 없고, 고정을 바꾸는 정의 변경은 접속 변경이다. PostgreSQL에는 egress의 등록 함수를 쓰고(pgsource가 `*tls.Config`를 쥐지 않는다) egress의 우회 검사는 `tls.Config` 필드 허용 목록의 AST 단언으로 하며, pgsource의 우회 검사는 등록 함수만 허락하도록 내용을 바꾼다(그 ADR의 `amends_tests`).
10. **E7. 앞 요청의 결과를 쓰는 자식 데이터셋**(C). 부모 값은 홑중괄호 변수 `{parent.x}`(경로 마디, 질의 값, 본문 값)로 읽고 겹중괄호 `{{`는 지금처럼 중괄호 글자 자체다(잠긴 `TestAdpDoubledBracesStandForTheBrace` 그대로). 부모 값을 먼저 끝까지 모은 뒤 자식을 요청한다(상한과 중복 제거, 넘으면 자식 요청 0건과 불완전). 404의 `on_not_found: skip`은 보인 것만이고 자식 요청의 5%와 3건을 넘으면 불완전이다. 경로는 `Path`와 `RawPath`로 한 번만 인코딩하고 부모 값은 ASCII 인쇄 가능 글자만이며 묶음 구분자가 든 값은 거절한다. 처음 한도(측정으로 고친다. 정의가 낮출 수 있고 서버 설정의 천장을 넘을 수 없다): 자식 데이터셋 하나의 요청 수 기본 2,000과 천장 50,000, 실행 하나의 전체 요청 천장 200,000, 데이터셋 시간 기본 30분과 천장 4시간, 실행 전체 시간 기본 2시간과 천장 6시간, 동시성 1, 묶음 기본 100과 최대 1,000, 부모 값 길이 256바이트, 주소 8 KiB, 본문 1 MiB, 미리 보기의 자식 요청 10건. 토큰 요청과 부모 다시 읽기를 포함한 모든 요청을 한도에 센다. 한도를 올리는 값은 접속 변경이다.
11. **E8. 레코드 안의 배열을 행으로 펼치기**(A2). `records_path`에 `[*]`를 한 번 쓸 수 있고(두 번은 정의 오류), 원소 레코드에서 바깥 레코드의 값은 `^.`(한 단계 위)로 읽는다. 자연 키는 원소의 필드와 `^.`의 필드로 적고 배열 안의 번호는 키로 쓰지 않는다. 바깥 레코드에 배열이 없거나 null이면 원소 0개이되 그 데이터셋은 보인 것만이고(`missing_array: "empty"`를 고르면 원소 0개로 완전), 바깥 레코드가 하나 이상인데 모두에 배열이 없으면 불완전이다. 페이지 넘김, 짧은 페이지, 전체 수, 이어 읽기 토큰은 바깥 레코드로 센다. `[*]`와 `^.`는 `ParsePath`를 바꾸지 않고 별도 읽기 함수로 한다(잠긴 문법 테스트 그대로). 적용은 HTTP의 `json`과 `json_lines`, 파일의 JSON Lines이고 PostgreSQL은 `sql`로 한다.
12. **오류 코드(결정 12).** 실행 중의 일은 기존 `SourceError` 12종과 고정 글만 쓴다. 새 정의 오류 코드는 잠긴 사례 표에 사례를 더하고 그 ADR의 `amends_tests`에 적는다(사례를 더할 뿐 빼거나 고치지 않는다). 이 PR은 새 정의 오류 코드를 만들지 않았고 기존 코드의 뜻을 넓혔으므로 그 표는 그대로다.

### 질문의 답 가운데 7절이 바꾸지 않은 것

- `text/plain` 토큰 응답은 넣지 않는다.
- 토큰 요청 본문도 데이터 요청과 같은 JSON 트리 엔진 하나를 쓴다(뜻의 구현은 하나).
- 시크릿의 범위가 필드 단위가 아닌 것을 받아들인다. 자격 증명은 토큰 호스트로만, 토큰은 API 호스트로만 가는 것은 어댑터 구조로 지키고, 편집자가 `{{credential.*}}`를 API 호스트로 가는 값에 넣어 시크릿을 보내는 일은 남는 위험으로 둔다(그 호스트도 시크릿 주인이 허락한 곳이다). 필드 단위 묶임은 시크릿 형식이 바뀌는 일이라 베타 뒤에 한다.
- 인증서 고정을 바꾸는 정의 변경은 접속 변경이고 감사 로그를 남긴다.
- 작업을 만들고 기다려 결과를 받는 흐름은 범위 밖이다(동기 내보내기와 `json_lines`로).

### PR의 나눔

다섯 개다: **A1(E1·E3, 권한 분류, 이 PR) → A2(E4·E8, 완전성의 보인 것만) → B1(E5·E6) → B2(E2) → C(E7)**. 이 ADR이 E1~E8의 결정 전체를 적고 뒤의 PR은 이것을 고치거나 채우는 짧은 ADR이다. 모두 독립 보안 검토를 거치고 B1이 가장 엄격하다(TLS 검증을 우회할 길을 새로 열고 잠긴 테스트의 내용을 바꾼다).

## 범위 밖과 그 대안

| 범위 밖 | 이유 | 대안 |
|---|---|---|
| 서명 인증(SigV4 따위) | 요청 전체에 서명을 계산해야 해서 자리표시로 안 된다 | 그 제품의 정적 키나 토큰 수단, 없으면 서명을 대신하는 중계 프록시나 내보내기 |
| 쿠키 세션과 CSRF | 상태 있는 세션과 응답 헤더의 재사용이 필요하다 | 그 제품의 토큰 API(세션 토큰을 주는 API는 E1로 된다) |
| XML 전용 API | 파서와 형식이 JSON을 가정한다 | JSON 엔드포인트가 있는 버전이나 프록시 변환 |
| 서명 JWT 클라이언트 인증(`private_key_jwt`) | 개인 키로 서명해야 한다 | 클라이언트 비밀을 본문이나 Basic으로 보내는 방식(E2) 또는 정적 토큰 |
| 클라이언트 인증서(mTLS) | 키 보관과 TLS 설정의 확장이 든다 | 같은 제품의 토큰 방식이나 인증서를 끝내는 프록시 |

## 호환의 변화(이 PR)

- 요청 본문에 자리표시를 쓰는 정의가 새로 가능하다. 반대로 `connection.headers`의 값에 자리표시 글자가 든 정의와, 어디든 `{{auth.token}}` 글자가 그대로 든 정의는 이제 정의 오류다(자리표시의 자리를 한 문법으로 묶었다).
- 토큰 요청의 본문은 글자 값의 평평한 맵에서 JSON 트리가 되었다(`form`은 여전히 글자 값의 평평한 맵만). `format`은 `basic`이고 본문이 없을 때 생략할 수 있다.
- `ClassifyChange`는 아직 부르는 곳이 없다.

## 남는 위험과 미룬 것

1. 정의를 편집할 수 있는 사람이 시크릿을 같은 출처 안에서 쓸 수 있는 길은 줄지 않았다. `auth.headers`와 본문 자리표시는 편집자가 시크릿이 가는 곳을 고르는 길이므로 접속 변경(이중 권한)으로 묶었다. 실제 묶임은 `ClassifyChange`를 부르는 정의 저장소가 들어와야 한다.
2. 가리기는 값 그대로와 정해진 인코딩 꼴만 찾는다. 서버가 다른 변환(해시, 일부만 되돌림)으로 돌려주는 값은 못 찾는다. 8바이트보다 짧은 비밀 필드는 행에서 가리지 않는다(행의 값이 바뀌어 가짜 변경을 만드는 것을 피하는 값이고, 사건과 미리 보기와 오류에서는 4바이트부터 가린다).
3. 검토에서 나온 R9(정규식 합치기)와 OAuth2의 `basic_form` 같은 확장은 뒤로 미뤘다.

## 확인

- `ingest`의 `hxa_defs_test.go`(문법, 본문 트리, 자리표시의 필요, 헤더 이름, Basic과 `token_from`, 권한 분류, 자리표시를 읽는 곳이 한 곳)와 `httpsource`의 `hxa_places_test.go`(가짜 서버가 받은 바이트, 줄바꿈과 제어 문자의 거절과 재시도 0번, 401 뒤 새 토큰, 되돌려 주는 서버로 가리기, 사용자 이름이 행을 바꾸지 않음).
- 잠긴 테스트: 위 둘의 지문만 바뀐다.
