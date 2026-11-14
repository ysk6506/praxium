---
title: ADR-0044 내장 인증 — 접근 토큰, 첫 관리자의 부트스트랩, 운영 모드의 시작 조건
kind: decision
status: active
enforced_by:
  - GOARCH010
  - GOARCH011
  - GOCOV001
  - TLOCK001
  - TRACE006
  - backend/internal/identity/auth_test.go
  - backend/internal/identity/bootstrap_test.go
  - backend/internal/identity/manage_test.go
  - backend/internal/identity/audit_test.go
  - backend/internal/identity/limiter_test.go
  - backend/internal/identitycheck/identitycheck_test.go
  - backend/internal/identityfile/store_test.go
  - backend/internal/pgstore/records_test.go
  - backend/internal/httpapi/identity_test.go
  - backend/internal/cli/admin_test.go
  - backend/cmd/praxium/auth_test.go
updates_docs:
  - ARCHITECTURE.md
  - docs/design-docs/access-control.md
  - docs/design-docs/rest-api.md
  - docs/design-docs/rest-api-feature-routes.md
  - docs/design-docs/rest-api-resources-graph.md
  - docs/design-docs/temporary-console.md
  - docs/exec-plans/active/plan.md
  - docs/exec-plans/tech-debt.yaml
  - docs/guides/operate-the-server.md
  - docs/product-specs/requirements.md
touches_protected:
  - docs/product-specs/requirements.md
  - harness/config/architecture.yaml
  - harness/config/test-lock.json
  - harness/config/coverage-floor.json
  - docs/design-docs/adr/0024-rest-contract-becomes-a-document-the-machine-keeps.md
  - docs/design-docs/adr/0027-reading-the-kg-through-the-api-and-the-command-line.md
amends:
  - adr: "0024"
    what: 운영 모드는 늘 시작을 거부하고 인증은 개발용 인증기뿐이라는 결과
  - adr: "0027"
    what: 운영 모드가 실제 인증기를 기다린다는 배선
amends_tests:
  - go:backend/cmd/praxium::TestProductionNamesWhatItLacks
  - go:backend/internal/httpapi::TestEveryAnswerFitsTheContract
  - go:backend/internal/httpapi::TestProblemsKeepTheirShapeAndCatalogue
  - go:backend/internal/httpapi::TestEveryOperationAsksForTheRoleItDeclares
  - go:backend/internal/httpapi::TestWithoutAnAuthenticatorOnlyTheOpenRoutesAnswer
  - go:backend/internal/pgstore::TestMigrationsAreRecordedAndAnAppliedOneIsNeverEdited
test_change_reason: 운영 모드가 시작하게 되어 늘 거부한다는 단언이 틀려졌고, 계약에 경로가 더해져 경로마다 쓰는 표본과 역할 단언이 달라져야 하며, 마이그레이션이 하나 더 생겨 단계의 번호와 개수를 고정값으로 쓴 테스트가 틀려졌다. 어느 단언도 약해지지 않았다.
decision_hash: ea29bbc0592b1efa
---
# ADR-0044 내장 인증 — 접근 토큰, 첫 관리자의 부트스트랩, 운영 모드의 시작 조건

## 맥락

인증은 개발용 인증기(`adapter.devauth`)뿐이다. 설정 파일이나 환경 변수에서 고정 토큰 하나를 받고, 운영 모드는 늘 시작을 거부한다(ADR-0024, ADR-0027, 부채 TD-0010). 그래서 프락시움은 개발 모드의 장난감으로만 돌고, 사람이 둘 이상인 조직이 쓸 수 없다.

프락시움은 범용 오픈소스 제품이고 셀프 호스팅이 우선이다. 단일 로그인(SSO)이 없는 조직도 사용자를 여럿 두어야 한다. 권한 설계(ADR-0012)는 SSO(OIDC)를 첫 어댑터로 정했지만, 그것이 없다고 운영 모드가 막혀서는 안 된다. 인증은 보안 기능이므로 지름길 없이 만들고, 확신이 없는 선택은 보수적인 쪽으로 정한다.

## 결정

**1. 범위.** 이 결정은 **인증**("누구인가")과 역할 판정의 **최소한**("읽을 수 있는가", "사용자와 토큰을 관리할 수 있는가")을 만든다. 판정은 기존 역할 표(`core.access`)를 그대로 쓴다. 범위를 가진 권한 부여, 객체 조건, 승인, OIDC는 하지 않는다. 다만 포트의 모양(`Subject`, `Authenticator`, `Directory`)은 그것들이 나중에 붙을 수 있게 둔다.

**2. 비밀번호가 없다.** 기본 계정과 기본 비밀번호는 없다. 로그인 화면도 세션 쿠키도 없다. 신원은 **접근 토큰**(API 키 방식) 하나로 증명하고 `Authorization: Bearer`로 보낸다. 사람의 비밀번호를 받지 않으므로 비밀번호를 약하게 고르거나 재사용하는 일, 비밀번호 해시의 유출, 로그인 시도를 막는 일을 이 제품이 떠안지 않는다.

**3. 토큰.**

| 항목 | 내용 |
|---|---|
| 형식 | `pxm_` + 토큰 ID(16자리 16진수) + `_` + 비밀(64자리 16진수, 256비트). 전체 85자. ID는 이름이고 비밀이 아니다 |
| 비밀 | 운영체제의 엔트로피(`crypto/rand`, `kernel.NewToken`)에서만 만든다. 시계나 `Rng`로 만들지 않는다 |
| 저장 | 서버는 SHA-256(`"praxium.token.v1" 0x00 ID 0x00 비밀`)의 16진수만 저장한다. 비밀과 토큰 전체는 저장하지 않는다 |
| 비교 | 상수 시간(`crypto/subtle`)이다. 모르는 ID에도 같은 길이의 가짜 해시와 비교해 시간으로 ID가 있는지 알 수 없다 |
| 소유자와 역할 | 토큰은 소유자(사용자나 서비스 계정), 역할, 설명을 가진다. 토큰이 실제로 가지는 역할은 **토큰의 역할과 소유자의 현재 역할의 교집합**이다. 토큰은 소유자의 권한을 줄일 수만 있다 |
| 수명 | 모든 토큰이 만료된다. 기본 90일, 가능한 값은 1시간~365일이다. 만료 없는 토큰은 없다 |
| 폐기 | 토큰은 폐기할 수 있고, 폐기하거나 만료한 토큰과 비활성화한 소유자의 토큰은 다음 요청부터 거부한다. 폐기를 되돌리지 않는다 |
| 마지막 사용 | 분 단위로 잘라 분당 최대 한 번만 쓴다(요청마다 쓰지 않는다). 실패해도 요청은 계속한다 |
| 평문 | 발급한 응답 하나에만 평문이 있다. 어떤 로그, 오류, 목록, 감사 로그에도 토큰과 비밀과 해시가 나오지 않는다(테스트가 막는다) |

**해시를 SHA-256 하나로 하는 이유.** 비밀번호 해시(bcrypt, scrypt, argon2)가 느린 것은 사람이 고른 낮은 엔트로피의 값을 사전으로 추측하는 속도를 늦추려는 것이다. 이 비밀은 사람이 고르지 않고 256비트 난수이므로 추측으로 찾는 것이 불가능하다. 이 경우 느린 해시는 보안을 더하지 않고 요청마다 CPU만 쓴다. 저장소가 유출돼도 해시에서 비밀을 되찾을 수 없다. 해시는 ID와 용도 접두어를 함께 넣어 다른 곳의 해시와 섞이지 않게 한다.

**4. 주체.** 주체는 사용자(사람)와 서비스 계정이다. 이름은 `^[a-z0-9][a-z0-9._@-]{0,63}$`이고 사용자와 서비스 계정을 통틀어 유일하며 바뀌지 않는다. 이름이 곧 주체의 ID다. 사용자는 `ingester`를 가질 수 없고 서비스 계정은 `admin`을 가질 수 없다. 삭제는 없고 비활성화만 있으며(되돌리지 않는다) 마지막 활성 관리자는 비활성화할 수 없다. 역할을 바꾸는 경로와 다시 활성화하는 경로는 만들지 않는다(후속).

**5. 부트스트랩.** 첫 관리자는 서버 기계에서 서버 없이 `praxium admin bootstrap -name <이름> -store file|postgres …`로 만든다. 토큰은 표준 출력에 한 줄로 한 번만 나오고 안내는 표준 오류로 나간다.

- **활성 관리자가 하나라도 있으면 거절한다.** 쓸 수 있는 토큰이 없어도 거절한다. 실행 중인 서버를 가로채는 데 쓸 수 없게 하는 엄격한 쪽이다. 토큰을 잃었으면 `praxium admin issue-token -owner <이름> …`으로 서버 기계에서 기존 주체의 새 토큰을 받는다. 이것도 서버 기계에서만 되고 감사 로그에 `(local operator)`가 만든 것으로 남는다.
- **동시에 두 부트스트랩이 성공할 수 없다.** 저장소의 `meta/bootstrap` 기록을 읽은 버전으로 선점하고(버전 비교 쓰기), 선점이 2분이 지나지 않았으면 다른 부트스트랩은 거절한다. 실패한 부트스트랩은 선점을 돌려주고, 죽은 부트스트랩의 선점은 2분 뒤 풀린다. 선점한 뒤 관리자가 있는지 다시 본다.
- 데이터 폴더(`-store file`)는 폴더의 잠금을 잡으므로 서버가 돌고 있지 않을 때만 된다. PostgreSQL은 잠금이 필요 없고 위의 선점이 배제를 맡는다.

**6. 저장은 KG와 다른 포트다.** 인증 데이터를 KG에 넣지 않는다. 세 구현이 같은 작은 적합성 테스트(`adapter.identitycheck`, 12사례, graphcheck와 같은 방식)를 통과한다.

| 구현 | 내용 |
|---|---|
| 포트 | 원시 타입만 쓴다: 읽기, 버전 비교 쓰기(없는 기록은 0), 종류별 훑기. 종류는 `subject`, `token`, `meta` |
| 메모리 | `core.identity`가 가진 구현. 테스트용이다. 운영 모드는 이것을 거부한다 |
| 파일 | 데이터 폴더의 `identity.json`. 형식 이름 `praxium-identity`과 버전이 있고 모르는 형식과 새 버전은 덮어쓰지 않고 거절한다. 쓰기는 임시 파일, `fsync`, 이름 바꾸기, 폴더 동기화다. 파일은 소유자만 읽고 쓴다(0600). 다른 사용자가 읽을 수 있는 파일은 열기를 거절하고 `chmod 600`을 알린다. 디스크는 `Disk` 인터페이스로만 만나고 구현은 `cmd.praxium`의 `authDisk`가 한다 |
| PostgreSQL | 마이그레이션 `0002_identity.sql`의 테이블 `identity_record`(종류, ID, 버전, 데이터). 같은 데이터베이스에 KG와 함께 있다 |

**단위.** 어댑터는 `core.access`와 `core.model` 말고는 import할 수 없으므로 포트와 값 타입(`Directory`, `FailureLimiter`, `Principal`, `TokenInfo`)은 `core.access`에 두고 구현은 새 유닛 `core.identity`(`core.kernel`, `core.access`만 import)에 둔다. 파일 스토어는 `adapter.identityfile`, 적합성 테스트는 `adapter.identitycheck`(테스트 파일만 세 구현을 import한다)다. 기존 유닛의 `may_import`는 넓히지 않았다.

**7. 운영 모드의 시작 조건.** `praxium api serve`(기본 모드가 운영이다)는 다음을 **스토어를 열기 전, 주소를 잡기 전에** 확인하고 하나라도 어긋나면 시작하지 않는다. 오류는 무엇을 하면 되는지 말한다.

| 조건 | 거절하는 것 |
|---|---|
| 스토어 | `memory`(기본값)이다. `-store file -store-data-dir`나 `-store postgres -store-dsn-file`이 있어야 한다 |
| 개발용 인증 | `-dev-*` 플래그, 환경 변수 `개발용 토큰 환경 변수`이 있다. 개발용 인증기와 데모, 로컬 모드의 약한 토큰은 운영에서 쓰이지 않는다 |
| 관리자 | 스토어에 활성 관리자가 없다. `praxium admin bootstrap`을 안내한다 |
| TLS | `-tls-cert`와 `-tls-key`가 하나만 있다. 루프백이 아닌 주소에서 TLS도 `-behind-proxy`도 없다(평문 HTTP로 토큰을 보내게 하지 않는다) |

TLS는 표준 라이브러리(`crypto/tls`)이고 최소 버전은 1.2다. `-behind-proxy`는 TLS를 끝내는 역방향 프록시가 앞에 있다는 운영자의 선언이고, 이때 호출자의 주소는 `X-Forwarded-For`의 **마지막** 값이다(프록시가 덧붙인 값이라 호출자가 속일 수 없다). 개발 모드, `demo`, `local`의 동작은 바뀌지 않는다. 인증 저장소가 응답하지 못하면 401이 아니라 **503**(`server.unavailable`)이다. 토큰이 틀렸다고 말하면 클라이언트가 멀쩡한 토큰을 버릴 수 있다. 권한 설계(access-control 3-5)가 그린 인증 캐시는 만들지 않았다. 요청마다 저장소를 읽는다(주체와 토큰, 두 번).

**8. 관리 API와 명령줄.**

| 경로 | 설명 | 역할 |
|---|---|---|
| `GET /v1/tokens/{token_id}` | 토큰 하나. id `current`는 이 요청의 토큰이고 호출자가 누구인지도 답한다(`praxium whoami`). 다른 id는 소유자와 관리자만 본다 | 로그인한 누구나 |
| `GET, POST /v1/users`, `POST /v1/users/{user_id}:disable` | 사용자 | `admin` |
| `GET, POST /v1/system-accounts`, `POST /v1/system-accounts/{account_id}:disable` | 서비스 계정(계획에 있던 경로 세 개를 구현) | `admin` |
| `GET, POST /v1/tokens`, `POST /v1/tokens/{token_id}:revoke` | 토큰. 목록과 폐기는 소유자 자신의 것이거나 관리자 | 로그인한 누구나(발급은 `admin`) |

- 계획의 `GET /v1/me`는 만들지 않았다. 경로 세그먼트 `me`가 단수여서 `harness/config/api-contract.yaml`의 단수 허용 목록을 바꿔야 한다. `/v1/tokens/current`도 같은 이유(API002)로 쓸 수 없어서 경로 변수 `{token_id}`에 `current`를 허용하는 방식으로 같은 일을 하고 `/v1/me`는 계획에 남긴다(권한 부여와 허용 작업 힌트를 담을 자리다).
- 만들고 행동하는 POST는 `Idempotency-Key`(8~128자)가 필수이다. 같은 키와 같은 본문이면 첫 응답을 10분 동안 다시 주고(프로세스 메모리), 같은 키에 다른 본문이면 `conflict.idempotency_key_reused`이다. **토큰을 발급한 응답은 저장하지 않으므로** 그 키는 다시 쓸 수 없다. 비밀이 한 응답에만 있게 하는 것이다. 실패한 호출은 키를 돌려준다.
- 응답은 `Cache-Control: no-store`이다. 목록은 ETag가 없다.
- 개발 서버(`Directory`가 없다)는 관리 경로에 `not_found.resource`를 답한다.
- 명령줄은 오프라인 명령 `admin bootstrap`, `admin issue-token`과 서버를 부르는 `whoami`, `admin user|account|token list|create|disable|revoke`이다. CLI의 출력과 오류는 영어이고 `--token` 대신 환경 변수나 설정 파일을 권한다.

**9. 감사 로그와 속도 제한.** 구조화 로그에 `audit: true`, `event`, `actor`를 단다: `bootstrap`, `bootstrap_refused`, `principal_created`, `principal_disabled`, `token_issued`, `token_revoked`, `auth_failed`, `auth_blocked`. 실패한 인증은 이유(`token_invalid`, `token_expired`, `access_token_revoked`, `access_subject_disabled`), 호출자의 주소, 경로, 추적 ID를 남기고 **제시된 토큰은 남기지 않는다**. 같은 주소(IPv6는 /64)가 연속으로 10번 토큰을 거절당하면 5분 동안 429(`rate_limited.requests`)와 `Retry-After`로 막는다. 헤더가 없는 요청은 세지 않고, 성공해도 이미 걸린 차단은 풀리지 않으며, 추적하는 주소는 최대 1만 개이다.

**10. 부채.** TD-0010을 닫는다. OIDC와 서비스 계정 토큰 교체는 부채가 아니라 후속 범위다.

**11. 미룬 것.** OIDC 연동, 범위를 가진 권한 부여와 객체 조건, 승인, 역할 바꾸기, 다시 활성화, 토큰 교체(`rotate-token`)와 서비스 계정의 `bound_persons`, 감사 로그 조회 API(`/v1/audit-records`), 인증 캐시, 여러 서버에 걸친 속도 제한이다.

### 못 고친 것: 잠긴 테스트와 부딪친 곳

| 테스트 | 왜 바꾸나 | 바뀐 것 |
|---|---|---|
| `cmd/praxium::TestProductionNamesWhatItLacks` | 운영 모드가 시작하게 되어 "늘 거부하고 TD-0010, TD-0011을 말한다"가 틀렸다 | 메모리 스토어의 운영 모드는 `-store file`과 `praxium admin bootstrap`을 안내하며 거부하고, 관리자 없는 스토어는 부트스트랩 명령을 알리며 거부한다고 단언한다. 부채 번호가 없다는 단언이 더해졌다 |
| `httpapi::TestEveryAnswerFitsTheContract`, `TestProblemsKeepTheirShapeAndCatalogue`, `TestWithoutAnAuthenticatorOnlyTheOpenRoutesAnswer` | 경로마다 표본을 요구하는 표(`pathSamples`, `requestSamples`)에 새 경로의 값이 있어야 한다 | 표에 새 경로의 값을 더했다(데이터만). 표본은 `viewer`에 디렉터리가 없는 서버이므로 관리자 경로는 403, 디렉터리가 필요한 두 경로는 404다. 성공 응답과 계약 적합성은 새 테스트 `identity_test.go`가 디렉터리와 함께 확인한다 |
| `httpapi::TestEveryOperationAsksForTheRoleItDeclares` | 마지막 분기가 "읽기 아닌 경로는 로그인한 누구나 200"인데 `admin` 경로는 403이다 | `admin` 경로는 익명 401, 그 밖의 역할 403을 단언하는 분기를 더했다. 마지막 분기는 `ingester`만이 아니라 `viewer`도 표본이 말하는 상태를 받는다고 강해졌다 |
| `pgstore::TestMigrationsAreRecordedAndAnAppliedOneIsNeverEdited` | 가짜 단계의 버전과 기록 개수를 2와 3으로 고정했는데 마이그레이션이 하나 더 있다 | 버전과 개수를 `len(steps)+1`, `len(steps)+2`로 쓴다. 같은 성질(고친 적용 단계는 거절, 실패한 단계는 흔적이 없다)을 단언한다 |

### 바뀐 설정과 보호 경로

| 경로 | 바뀐 것 | 이유 |
|---|---|---|
| `harness/config/architecture.yaml` | 유닛 `core.identity`, `adapter.identityfile`, `adapter.identitycheck` 등록, `locked_support`에 `backend/internal/identitycheck` 추가 | 새 유닛의 등록. 적합성 사례는 graphcheck와 같이 잠긴 테스트의 지문에 들어가 사례가 약해지면 잠금이 깨진다 |
| `harness/config/coverage-floor.json` | 새 세 유닛의 하한 | 새 유닛의 등록 |
| `harness/config/test-lock.json` | 새 테스트의 지문, 위 여섯 테스트의 지문 | `pnpm lock-tests`, `--amend` |
| ADR-0024, ADR-0027 | `amended_by`와 수정한 부분 | 이 결정이 일부를 고친다 |

`harness/config`의 규칙, 한도, 기존 유닛의 `may_import`, 허용 목록, `api-contract.yaml`은 바꾸지 않았다. 새 의존성은 없다(표준 라이브러리뿐이다).

## 이유

- 비밀번호를 받지 않으면 가장 흔한 사고(약한 비밀번호, 재사용, 해시 유출)가 이 제품의 몫이 아니다. 접근 토큰은 사람과 프로그램에 같은 방식이고, 폐기와 만료가 쉬워서 SSO가 없는 조직에도 맞다.
- 첫 관리자를 서버 기계에서만 만들면 "기본 계정"이 없고, 로컬 접근이 곧 전권이라는 신뢰 경계가 분명하다. 엄격한 거절은 편의보다 안전을 택한 것이고, 잠겼을 때의 길(`issue-token`)을 따로 둬서 막다른 길을 없앴다.
- 인증 데이터를 KG와 다른 포트에 두면 KG의 변경 시퀀스, 감사, 리버트 규칙에 섞이지 않는다. 세 구현이 같은 사례를 통과하므로 어느 스토어를 골라도 같은 규칙이다.
- 시작 조건을 스토어를 열기 전에 확인하면 잘못된 설정이 데이터 폴더를 만들거나 포트를 잡은 뒤에 드러나지 않는다.

## 대안과 기각 사유

| 대안 | 기각 사유 |
|---|---|
| 비밀번호 로그인과 세션 쿠키 | 비밀번호 저장, 로그인 시도 제한, 쿠키와 CSRF를 모두 떠안는다. 프로그램에는 어차피 토큰이 필요하다 |
| 토큰에 느린 해시(argon2 등) | 256비트 난수에는 보안을 더하지 않고 요청마다 비용만 든다. 서드파티 의존성도 필요하다 |
| JWT(서명한 토큰) | 폐기가 즉시가 아니고 키 관리가 필요하다. 토큰마다 저장소의 기록을 읽으므로 상태가 있는 토큰이 더 단순하다 |
| 환경 변수에 관리자 토큰을 두기 | 프로세스 목록과 쉘 기록에 남는다. 기본 계정과 다르지 않다 |
| 부트스트랩을 HTTP 경로로 열기(첫 호출이 관리자가 된다) | 서버를 먼저 띄우는 쪽이 이긴다. 오프라인 명령이 안전하다 |
| 관리자가 있어도 부트스트랩이 새 관리자를 더하게 하기 | 서버 기계에서 읽기 전용으로만 접근하는 사람이 관리자를 만든다. 복구는 `issue-token`으로 충분하다 |
| 인증 캐시 | 폐기가 늦게 반영된다. 규모가 필요할 때 더한다. 지금은 저장소가 장애이면 503으로 닫힌다 |
| `GET /v1/me` | 단수 세그먼트 허용 목록(보호 경로)을 바꿔야 한다. `GET /v1/tokens/current`도 단수 세그먼트다. 경로 변수 `{token_id}`의 값 `current`로 같은 일을 한다 |
| 토큰의 무기한 허용 | 끝나지 않는 유출이 된다. 365일을 한도로 두고 기본은 90일이다 |

## 결과

- `praxium api serve`가 운영 모드로 시작한다. 처음 쓰는 순서는 `admin bootstrap`, `api serve`, `admin user create`, `admin token create`이다. 운영 안내서는 `docs/guides/operate-the-server.md`다.
- REQ-307~314를 추가하고 REQ-160(SSO 부분만 예정으로 남긴다), REQ-161, REQ-260을 고친다. REQ-165는 그대로다.
- 마이그레이션 `0002`가 생겼다. 이미 적용한 마이그레이션은 고치지 않는다는 규칙은 그대로다.

### 위협과 남은 약점

| 약점 | 설명 |
|---|---|
| 토큰의 탈취 | 토큰을 가진 사람이 곧 그 주체다. 만료(최대 365일)와 폐기로 한정한다. 토큰의 ID는 로그와 목록에 있고 비밀은 없다 |
| 파일을 쓸 수 있는 사람 | `identity.json`이나 데이터베이스에 쓸 수 있는 사람은 자기 해시를 넣어 관리자가 된다. 해시가 있다고 토큰을 만들 수는 없지만 쓰기 권한이 곧 전권이다. 0600과 데이터 폴더의 소유권이 경계다 |
| 서버 기계의 접근 | `bootstrap`과 `issue-token`을 실행할 수 있으면 전권이다. 의도한 신뢰 경계이다 |
| 속도 제한 | 프로세스 메모리에 있어 재시작하면 지워지고 서버가 여럿이면 합쳐지지 않는다. 주소는 헤더가 아닌 연결의 주소이거나 `-behind-proxy`의 마지막 `X-Forwarded-For`다. 프록시를 잘못 선언하면 주소를 속일 수 있다 |
| 프록시 뒤 | `-behind-proxy`는 프록시와 서버 사이가 안전하다는 운영자의 선언이다. 서버가 그것을 확인하지 않는다 |
| 멱등 캐시 | 프로세스 메모리이고 재시작하면 지워진다. 토큰 발급의 키는 애초에 재사용되지 않는다 |
| 저장소의 부하 | 요청마다 저장소를 읽는다. PostgreSQL에서는 요청마다 두 번이다. 캐시는 후속이다 |
| 관리자 토큰 하나가 전권 | 관리자 사이의 권한 분리가 없다. 관리자 토큰은 짧은 만료와 작은 역할(토큰은 역할을 줄일 수 있다)로 쓴다 |
| 토큰 비교의 시간 | 상수 시간 비교와 가짜 해시로 줄였고 테스트가 비교 함수를 확인한다. 네트워크 위의 시간 측정으로 증명하지는 않는다 |
| `--token` 플래그 | 프로세스 목록에 보인다. 환경 변수나 설정 파일을 권하지만 막지는 않는다 |
| 웹 UI | 접속 화면은 토큰을 입력받아 `Authorization`에 싣는다(운영 토큰이 이미 통한다). 브라우저는 토큰을 `localStorage`에 둘 수 있다(XSS의 영향). 화면은 이 결정에서 바꾸지 않았다 |
