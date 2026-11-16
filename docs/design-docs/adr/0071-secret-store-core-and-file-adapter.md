---
title: ADR-0071 시크릿 저장소의 코어와 파일 어댑터 — 엔벨로프 암호화, 쓰기 전용 값, 소스의 범위와 허용 주소에 묶기
kind: decision
status: active
enforced_by:
  - GOARCH010
  - GOARCH052
  - backend/internal/secrets/secrets_test.go
  - backend/internal/secrets/tamper_test.go
  - backend/internal/secrets/keys_test.go
  - backend/internal/secrets/use_test.go
  - backend/internal/secrets/redact_test.go
  - backend/internal/secrets/ports_test.go
  - backend/internal/secretsfile/store_test.go
touches_protected:
  - harness/config/architecture.yaml
  - harness/config/coverage-floor.json
  - harness/config/test-lock.json
  - docs/design-docs/adr/0018-secrets-and-encryption.md
  - docs/product-specs/requirements-access.md
  - docs/product-specs/requirements.md
updates_docs:
  - ARCHITECTURE.md
  - docs/design-docs/secrets-and-encryption.md
  - docs/product-specs/requirements-access.md
  - docs/product-specs/requirements.md
amends_tests: []
amends:
  - adr: "0018"
    what: 결정 2의 래핑과 언래핑의 분리(이 구현은 대칭 키 하나), 결정 13의 알파 단계 가운데 이번에 하는 것
decision_hash: 89e0784b71cfe8b9
---
# ADR-0071 시크릿 저장소의 코어와 파일 어댑터

## 맥락

DB와 API를 소스로 붙이려면 접속 비밀번호와 토큰을 보관해야 한다. ADR-0018은 엔벨로프 암호화를 정했지만 코드가 없다. 이 변경은 가장 작은 조각을 만든다: 코어 유닛, 파일 어댑터, 암호화. REST 경로, CLI 명령, 수집과의 연결은 뒤의 변경이 붙인다.

## 결정

1. **유닛.** `core.secrets`(`backend/internal/secrets`, `may_import`는 `core.kernel`뿐)와 파일 어댑터 `adapter.secretsfile`(`core.kernel`, `core.secrets`). 코어는 표준 라이브러리의 `crypto/aes`, `crypto/cipher`만 쓰고 `os`와 `crypto/rand`를 import하지 않는다.
2. **시크릿.** `id`는 영문 소문자로 시작하고 소문자, 숫자, `_`, `-`로 64자까지다. 종류는 `password`(`username`, `password`)와 `token`(`token`)이고 필드는 종류가 정한 것과 정확히 같아야 하며 값은 비어 있지 않다(16 KiB까지, UTF-8). 버전은 1부터고 새 버전은 가장 높은 번호 다음이다. 열기가 주는 것은 폐기하지 않은 가장 높은 번호의 버전이다(새 버전을 폐기하면 이전 버전으로 돌아간다. ADR-0018 결정 6의 복귀). 폐기한 버전은 암호문을 지우고 메타만 남아 다시 열리지 않는다. **범위**는 소스 id의 목록, **허용 주소**는 호스트와 포트의 목록이고, 빈 목록은 아무도 쓰지 못한다는 뜻이다.
3. **포트 셋.** `Writer`(만들기, 새 버전, 버전 폐기, 삭제, 범위와 허용 주소 바꾸기), `Opener`(열기), `Describer`(목록, 하나 읽기: 값 없이 이름, 종류, 필드 이름, 버전, 때, 범위, 허용 주소)는 인터페이스 셋이다. 구현 `Service` 하나가 셋을 다 갖고 조립 지점이 역할마다 필요한 것만 넘긴다. 저장 포트 `Store`는 암호문만 든 레코드를 다룬다. 삭제와 폐기는 호출자가 "소스가 쓰는 중인가"를 확인한 뒤 부른다(이 유닛은 소스를 모른다).
4. **열기.** 쓰임 `Use`(소스, 호스트, 포트, 목적: 실행, 점검, 소스 탐색)를 받는다. 순서는 마스터 키, 묶음의 검증, 쓰임의 확인(`authorize` 한 곳: 범위, 주소), 데이터 키 풀기, 복호화다. 믿을 수 있는 범위와 주소를 읽으려면 묶음이 먼저 검증돼야 해서 키가 없으면 거절 이유도 알 수 없다. 거절은 `secret_scope_denied`, `secret_address_denied`다. 호스트의 비교 규칙: 이름은 ASCII 소문자로 비교하고 끝의 점 하나는 없는 것과 같다. IP는 파싱해 표준 표기로 쓰고 IPv6는 소문자와 압축형, URL의 대괄호는 벗긴다. IPv4-mapped IPv6는 그 IPv4와 같다. 영역(`%eth0`), 앞에 0이 붙은 IPv4, 마지막 레이블이 숫자나 `0x`인 이름(`127.1`), 포트, 경로, 공백, 와일드카드는 거절한다. 포트는 1~65535고 정확히 같아야 한다. 목적은 이름 셋 중 하나인지만 확인한다(기록은 뒤의 변경).
5. **감싼 타입.** 열기는 `Opened`를 주고 필드는 `Value`다. `Value`, `Opened`, 들어오는 값을 담는 `Fields`, `MasterKey`의 `String`, `GoString`, `Format`(모든 동사), `MarshalJSON`, `MarshalText`, `LogValue`가 `[REDACTED]`를 낸다. 맨 값은 `Value.Reveal()`뿐이다. fmt는 안 보이는 필드의 메서드를 부르지 않고 틀린 동사에서는 포인터가 가리키는 구조를 풀어 찍으므로, `Value`와 `Fields`는 값을 함수 값(클로저)에 담아 어느 길에서도 주소로만 보인다. `Close`가 바이트를 덮어쓴다(완전한 삭제는 보장하지 못한다. 설계 문서 16절).
6. **암호화.** 버전마다 새 32바이트 데이터 키가 필드의 모음(개수, 이름과 값의 길이 접두 인코딩)을 AES-256-GCM(96비트 난수 nonce, 128비트 태그)으로 **한 번** 암호화하고, 데이터 키를 마스터 키(32바이트)로 AES-256-GCM으로 감싼다. 인증 자료는 길이 접두 조각의 연결이고 맨 앞에 용도의 이름이 있다. 값: (`praxium-secrets/1/value`, 시크릿 id, 버전). 감싼 데이터 키: (`…/data-key`, 시크릿 id, 버전, 마스터 키 id). 용도와 판 번호가 인증 자료에 있어 한 용도의 암호문이 다른 용도로 풀리지 않는다.
7. **nonce의 구조.** 데이터 키는 `dataKey` 타입이 한 번만 암호화하게 막고(두 번째는 오류, 풀어낸 키는 암호화하지 못한다) 마스터 키 밖으로 나가지 못한다. 마스터 키는 감싸기(버전마다 하나)와 묶음의 태그(바꿀 때마다 하나)에 난수 nonce를 쓴다. 한 키로 난수 nonce를 2^32번 이하로 쓰라는 NIST SP 800-38D의 한도에 한참 못 미친다. 이 한도에 가까워질 키 교체는 뒤의 변경이다.
8. **묶기: 범위와 허용 주소를 손으로 고친 파일을 막는 방법.** 시크릿마다 **묶음 태그**를 둔다: 마스터 키로 빈 평문을 AES-256-GCM으로 암호화하고(GMAC) 인증 자료에 용도(`…/binding`), 시크릿 id, 종류, 마스터 키 id, 범위, 허용 주소를 순서대로 넣는다. 열기와 범위·주소를 바꾸는 쓰기와 새 버전 쓰기는 먼저 태그를 검증하고 틀리면 `secret_integrity_failed`다. 그래서 손으로 고친 범위나 주소는 열리지 않고, 바꾸는 쓰기가 고친 내용을 정상으로 만들지도 못한다. 값의 인증 자료에 넣지 않은 이유는 범위를 바꿀 때마다 값을 다시 암호화해야 하고 그것은 데이터 키를 한 번만 쓰는 구조와 맞지 않기 때문이다. **막지 못하는 것:** 파일 전체를 옛 사본으로 바꿔 놓기, 가장 새 버전을 파일에서 삭제해 옛 버전이 열리게 하기, 메타 포트가 키 없이 읽는 파일의 내용(고친 범위가 그대로 보인다. 열기가 막는다)이다. 이것은 백업을 복원하는 것과 구별할 수 없고, 막으려면 파일 밖의 상태(KMS, 감사 로그)가 필요하다.
9. **저장 형식.** `secrets.json` 하나: `{"format":"praxium-secrets","version":1,"secrets":[…]}`. 시크릿마다 `id`, `kind`, `created_at`, `scope`, `allowed_hosts`, `binding`, `versions`. 암호문 조각은 `{"method":"aes-256-gcm","key_id":…,"nonce":…,"data":…}`(바이트는 base64)이고 `key_id`는 마스터 키가 감싼 것(감싼 키, 묶음)에만 있다. 방식의 이름과 키 id가 있어 키 쌍이나 바깥의 키 관리로 옮길 수 있다. 정의 저장소의 파일 어댑터와 같은 쓰기(임시 파일, 동기화, 이름 바꾸기, 폴더 동기화), 모르는 판은 `unsupported`, 모르는 필드와 모양이 틀린 레코드는 거절하고 파일은 덮어쓰지 않는다.
10. **마스터 키.** 코어는 `KeyProvider`로 받는다. 키 파일은 `{"format":"praxium-master-key","version":1,"key_id":…,"method":"aes-256-gcm","key":base64}`이고 `ParseMasterKey`, `GenerateMasterKey`(`kernel.ByteSource`에서 32바이트와 id `mk-` + 16자리 16진수), `Encode`가 있다. 오류 코드는 셋이다: `secret_master_key_missing`(키 없음. 목록과 읽기, 폐기와 삭제는 키 없이 된다), `secret_master_key_invalid`(형식이 틀림. 메시지에 입력을 싣지 않는다), `secret_master_key_mismatch`(저장된 시크릿을 다른 id의 키가 감쌌음. 같은 id에 다른 바이트면 인증 실패다). 문장은 시크릿을 다시 넣거나 맞는 키 파일을 지정하라는 길을 말한다.
11. **난수.** `kernel`에 포트 `ByteSource`와 구현 `SystemBytes`(`crypto/rand`, `entropy.go`)를 더하고 테스트용 결정적 `SeededBytes`를 둔다. 고정된 난수, 시계, 키를 넣으면 저장된 바이트까지 같다.
12. **ADR-0018 결정 2의 예외.** 결정 2는 키 파일 방식에서 키 쌍으로 래핑과 언래핑을 분리한다고 했다. 이 구현은 대칭 키 하나다. 한 프로세스가 쓰기와 열기를 다 하는 지금은 분리해도 얻는 것이 없고, 분리는 키 쌍이나 바깥의 키 관리로 옮길 때 한다(저장 형식이 방식의 이름을 갖는다).
13. **지시서가 정하지 않아 고른 것(가장 보수적인 표준).** 필드 모음의 인코딩, 묶음 태그를 GMAC으로 하는 것, 용도의 이름, 키 id의 모양, 같은 키로 감싸기와 묶음 태그를 함께 쓰는 것(인증 자료의 용도 이름으로 가른다), 키 파일의 JSON.

## 이유

- 값의 인증 자료에 시크릿 id와 버전이 있어 DB를 고치는 사람이 암호문을 다른 곳에 붙여도 풀리지 않는다.
- 범위와 주소는 값이 아니라 "누가 어디에 쓰나"의 규칙이다. 파일의 그 두 줄을 고쳐 다른 소스나 주소로 값을 보내는 것이 가장 쉬운 공격이라 값과 같은 힘으로 묶는다.
- 포트를 셋으로 가르면 조립 지점이 역할에 필요한 것만 넘기고, 메타 포트의 반환 타입에는 값을 담을 자리가 없다(테스트가 타입을 훑는다).

## 대안과 기각 사유

- **범위와 주소를 값의 인증 자료에 넣는다.** 기각. 위의 8.
- **HMAC-SHA-256으로 묶음 태그를 만든다.** 기각. 키 파생이 필요하고 쓰는 기본 요소가 하나 더 는다.
- **ChaCha20-Poly1305.** 기각. 지시서의 AES-256-GCM과 표준 라이브러리만으로 충분하다.
- **복호화한 값을 캐시한다.** 기각(ADR-0018, 설계 문서 6절).

## 결과

- 요구사항 REQ-403~406을 `requirements-access.md`에 더한다. REQ-210~217의 "예정" 표시는 그대로다. 문장이 겹치는 곳: REQ-210(평문 아님, 쓰기 전용), REQ-211(엔벨로프 암호화 스토리지), REQ-313(토큰과 비밀이 로그와 응답에 없다). 403~406은 이 구현의 구체적인 동작이고 앞의 것들을 대신하지 않는다.
- `ARCHITECTURE.md`의 유닛 표와 새 유닛의 커버리지 하한을 더하고, 설계 문서 15절에 이번에 구현한 것을 적는다.
- **이 변경에 넣지 않은 것:** REST 경로, CLI 명령과 플래그, 키 파일의 위치와 만들기, 수집(`core.ingest`)과 시작점의 명령과의 연결, 마스터 키의 교체, 바깥의 키 관리(KMS), KG별 해시 키, 감사 로그와 복호화 사용 기록, 지문과 만료, `certificate` 같은 다른 종류.
