---
title: ADR-0132 정의 가져오기 명령줄(ingest import), 마스터 키만 만들기(secret keygen), 명시한 키 파일의 링크 하나
kind: decision
status: active
enforced_by:
  - backend/cmd/praxium/ingestimport_test.go
  - backend/cmd/praxium/secretkeylink_test.go
  - backend/cmd/praxium/secretkey_test.go
  - backend/internal/ingest/dset_test.go
touches_protected:
  - harness/config/test-lock.json
  - docs/product-specs/requirements-definitions.md
  - docs/product-specs/requirements-access.md
  - docs/design-docs/adr/0076-secret-store-assembly-key-file-and-command-line.md
updates_docs:
  - docs/guides/operate-the-server.md
  - docs/design-docs/source-editing.md
  - docs/design-docs/ingestion-service.md
  - docs/product-specs/requirements-definitions.md
  - docs/product-specs/requirements-access.md
amends_tests: []
amends:
  - adr: "0076"
    what: 결정 4의 마지막 요소가 링크인 키 파일을 읽지 않음과 결정 10 한계의 둘째 중 링크인 키 파일의 거절 — -secrets-key-file로 명시한 키 파일은 링크 하나를 따라간다
decision_hash: 57d652d320e1021b
---
# ADR-0132 정의 가져오기 명령줄, 마스터 키만 만들기, 명시한 키 파일의 링크 하나

## 맥락

서버가 소스를 수집하려면(ADR-0058, ADR-0086) 소스 정의와 매핑 정의가 정의 저장소(ADR-0049)에 있어야 하고, 소스에 접속하는 시크릿이 마스터 키 파일로 열려야 한다. 지금은 셋 다 막혀 있다.

- 정의 저장소는 종류를 모르는 저장소이고, 조직 속성(`type_overlay`, ADR-0053)만 그 안에 들어간다. 소스와 매핑을 넣는 제품 코드가 없다(REQ-438, REQ-476은 예정이었다).
- 정의 쓰기는 명령줄(쿠버네티스에서는 파드 안에서 실행하는 명령)만 하고 서버는 읽기만 한다는 것이 설계 책임자의 결정이다(I-8 결정 D1). 그러니 명령줄의 가져오기가 먼저 있어야 한다.
- 마스터 키는 시크릿을 처음 넣을 때 만들어진다(ADR-0076). 키를 오케스트레이터의 시크릿에 먼저 넣고 서버가 그 자리를 읽게 하려면 키 파일만 먼저 만드는 길이 필요하다.
- 오케스트레이터의 시크릿 볼륨은 파일을 링크로 준다(`master.key` → `..data/master.key`, `..data` → 그때의 폴더). ADR-0076 결정 10은 링크인 키 파일을 거절하고 "서버가 저장소를 여는 단계에서 다시 정한다"고 남겼다. 설계 책임자는 명시한 경로의 마지막 링크 하나만 따라가고 `mode & 0077` 기준은 그대로 두기로 정했다(D8).

나눔에서 이 변경은 I-8a이고 계약(OpenAPI)과 스키마는 바뀌지 않는다. 서버가 저장소를 읽는 길(I-8b), REST, 소스·매핑의 삭제와 내보내기는 이 변경에 없다.

## 결정

1. **`praxium ingest import -data-dir <폴더> -source <파일> -mapping <파일> [-message <글>]`.** 소스 정의(파일, http, postgres 어댑터 — `ParseAdapterSource`)와 매핑 정의를 종류 `source`와 `mapping`의 항목으로 저장한다(문서 하나가 항목 하나, 이름은 문서의 id, 항목의 스키마 버전 1, 정의 세트의 출처 `import`). 쓰기는 `model apply`와 같은 길이다: 데이터 폴더의 잠금, 머리 번호를 조건으로 한 원자적 쓰기, 같은 내용이면 쓰지 않음(`Apply`의 "바뀌지 않음", 종료 코드 0). 같은 이름의 항목이 있으면 그 항목의 새 리비전이다. 다른 프로세스가 잠금을 쥐고 있으면 서버를 멈추고 다시 하라고 답한다.
2. **매핑의 id.** id가 없는 옛 매핑은 소스의 id를 받고, 그 id를 문서에 넣어 저장한다(문서가 자기 이름을 말한다). 다시 가져오면 같은 항목이 바뀐다(REQ-476).
3. **저장하기 전의 컴파일(`core.ingest`의 `CompileSet`).** 저장소 머리의 모든 소스와 매핑에 두 문서를 얹은 세트를 새 흐름의 컴파일(`CompileScale`)로 컴파일하고 하나라도 깨지면 아무것도 쓰지 않는다. 여러 소스를 한 계획으로 읽는 길(`<소스 id>.<데이터셋 id>`, ADR-0075가 뒤로 미룬 6b)이 아직 없으므로 매핑 하나는 소스 하나와 짝지어 컴파일한다:
   - 매핑과 id가 같은 소스가 매핑이 읽는 데이터셋을 정의하면(또는 어느 소스도 그 데이터셋을 모두 정의하지 않으면) 그 소스. 옛 매핑이 소스의 id를 받기 때문이고, 그 소스에 없는 데이터셋은 컴파일이 자리와 함께 말한다.
   - 아니면 매핑이 읽는 데이터셋(피드의 `dataset`과 정제의 `input` 가운데 자기 정제가 아닌 것)을 모두 정의하는 소스가 하나뿐일 때 그 소스.
   - 없으면 `mapping.source.missing`, 데이터셋이 여러 소스에 걸치면 `mapping.source.mixed`, 여럿이 다 정의하면 `mapping.source.ambiguous`.
   - 피드, 정제, 링크 정의의 id는 매핑 사이에서 겹치지 않는다(`mapping.feed.id_taken`, `mapping.refinement.id_taken`, `mapping.link.id_taken`. 문장이 두 매핑을 모두 말한다). 매핑은 id 순서로 보고 먼저 본 매핑이 그 id를 가진다. 결과는 문서의 순서와 상관없다.
   - 오류는 ADR-0070의 형식에 문서의 id를 더한 `SetError`(`name`)이고 한 줄은 "<종류> <id> at <자리>: <문장>"이다. 한 번에 100개까지다.
   - 새 코드는 따로 된 표(`SetErrorCodes`)에 둔다. 첫 표의 잠긴 테스트(`TestEveryCodeOfTheTableIsGivenByACaseAtItsPlace`)는 문서 하나씩 돌리는 사례로 표의 모든 코드를 내야 하므로, 세트에서만 나는 코드를 넣을 수 없다. 새 흐름의 코드를 `ScaleErrorCodes`로 나눈 것과 같은 이유다. 새 표의 테스트가 다른 표와 겹치지 않음과 모든 코드가 사례로 나옴을 본다.
4. **저장소가 이미 가진 것.** 같은 종류에 같은 이름의 항목이 둘이면(저장소는 막지 않는다), 항목의 이름과 문서의 id가 다르면, 저장된 문서가 이 빌드에서 읽히지 않으면 그 항목을 말하며 거절한다. 조직 속성이 모델에 맞지 않으면 먼저 `model apply`로 고치라고 답한다(매핑이 조직 속성을 쓸 수 있어서 컴파일이 그 레지스트리로 하기 때문이다). 조직 속성을 읽는 쪽(`overlaysOf`)은 이미 종류로 거르고, 가져오기는 `source`와 `mapping`만 본다. 둘이 같은 저장소에 있어도 서로를 깨지 않음을 테스트로 잠근다.
5. **값처럼 보이는 것의 거절(`core.ingest`의 `SecretValues`, REQ-440의 확실한 것).** 두 파일을 정의로 읽기 **전에** 본다. 다른 검사의 문장(예: 주소를 되풀이하는 문장)이 값을 싣지 않게 하려는 것이다.
   - 이름 목록: `password`, `passwd`, `token`, `access_token`, `refresh_token`, `api_key`, `apikey`, `x_api_key`, `secret`, `client_secret`, `authorization`(소문자로, `-`를 `_`로 읽은 **온 이름**). 이름이 낱말을 품기만 하는 것(`token_path`, `is_secret`)은 아니다.
   - 문자열 속의 `이름=값`(`=` 양쪽에 빈칸 없음), 소스 정의에서 그 이름의 필드에 적은 글(헤더, 질의, 요청 본문), `스킴://계정@` 꼴의 주소, 개인 키의 시작 줄(`-----BEGIN … PRIVATE KEY-----`). 값이 비었거나 자리표시(`{{…}}`)나 SQL의 매개변수(`?`, `$`, `:`, `%`로 시작)이면 거절하지 않는다. 매핑의 필드 이름 규칙은 쓰지 않는다(`map`의 키가 데이터의 값이다).
   - 헤더의 거절 목록(`Authorization` 등)은 소스를 읽는 검사(`headerProblem`)가 이미 거절한다.
   - 오류는 파일, 자리(JSON 포인터), 이름만 말한다. 코드는 `definition.secret.name_value`, `definition.secret.field_value`, `definition.secret.address_account`, `definition.secret.private_key`.
   - 없는 시크릿을 가리키는 참조는 저장한다.
6. **`praxium secret keygen -secrets-key-file <파일> [-data-dir <폴더>] [--json]`.** 이름은 지시의 `-key-file` 대신 시크릿 명령들이 이미 쓰는 `-secrets-key-file`이다. 마스터 키 파일 하나만 만든다: 시스템의 난수, 0600, 없는 폴더는 0700, 그 자리에 파일이나 링크가 있으면 거절(임시 파일을 이름에 하드 링크하는 `putFileOnce`라서 동시에 만든 것도 덮지 않는다). 데이터 폴더가 없는 기계에서도 만들 수 있게 `-data-dir`은 선택이고, 주면 그 폴더가 있어야 하며 그 안의 자리를 거절한다(키 파일은 어느 명령도 데이터 폴더 안에서 쓰지 않으므로 빠뜨려도 키가 폴더와 함께 쓰이지는 않는다). 출력은 경로와 키 id다.
7. **명시한 키 파일의 링크 하나(D8).** `-secrets-key-file`로 명시한 마스터 키 파일과 웹훅 키 파일이 링크이면(`keyPath`, `followNamedLink`) 링크 하나를 따라간다. 링크 자리와 대상 둘 다 데이터 폴더 밖이어야 하고, 대상의 마지막 부분이 다시 링크이면 거절하고, 대상이 없으면 거절한다(링크를 거쳐 키를 만들지 않는다). 대상까지 가는 길의 폴더 링크(`..data`)는 다른 경로처럼 푼다. 대상은 다른 키 파일처럼 `readFileChecked`로 읽는다: 보통 파일, `O_NOFOLLOW`, 연 파일이 본 파일과 같음, 소유자만 읽음(`mode & 0077 == 0`), 크기 한도. **기본 위치의 키 파일과 데이터 폴더 안의 파일(`secrets.json`, `FOLDER_ID`)은 링크를 계속 거절한다.** 잠긴 테스트(`secretkey_test.go`의 "a link to the key is not followed", "an identifier that is a link")는 고치지 않고 통과한다.

## 이유

- 가져오기를 모델 명령과 같은 저장 길로 두면 잠금, 원자적 쓰기, 멱등, 출처의 규칙을 다시 만들지 않는다. 서버가 읽기만 하는 결정(D1)과도 맞는다.
- 저장소 전체를 컴파일하지 않고 새 문서만 보면, 다른 매핑과 피드 id가 겹치거나 저장된 매핑이 읽던 소스를 바꿔 깨는 일을 저장 뒤에야 안다. 머리 정의 세트는 언제나 컴파일된다는 설계([정의의 쓰기](../source-editing.md) 1.2)를 명령줄에서도 지킨다.
- 새 흐름의 컴파일을 쓰는 것은 저장소의 정의를 읽을 곳이 새 흐름(서버, PostgreSQL)이기 때문이다. 지금의 컴파일이 받는 정의는 새 흐름의 컴파일도 같은 계획으로 받는다(잠긴 테스트가 있다).
- 짝짓기를 데이터셋으로 정하면 저장하는 것 외에 짝의 기록이 필요 없고, 여러 소스의 계획이 오면 `<소스 id>.<데이터셋 id>`가 같은 정보를 문서 안에 적는다. id가 같은 소스를 먼저 보는 것은 옛 파일(매핑 id = 소스 id)의 관계를 그대로 살리는 것이다.
- 값의 검사를 파싱 전에 두면 다른 오류 문장이 값을 실을 길이 없다. 이름은 온 이름만 보아 `token_path` 같은 정의 필드를 거절하지 않는다.
- 링크는 명시한 경로에서 한 번만 따라가므로 사람이 정한 자리만 넓어지고, 대상의 권한과 위치는 그대로 검사한다. 기본 위치는 사람이 고른 것이 아니므로 그대로 막는다.

## 대안과 기각 사유

- **지금의 컴파일(`Compile`)로 검사**: 저장소의 정의를 읽는 새 흐름이 받는 문맥 키를 거절한다. 기각.
- **짝을 별도 항목이나 이름 규칙(매핑 id = 소스 id만)으로 저장**: 매핑이 자기 id를 가질 수 있다는 ADR-0075와 맞지 않고, 여러 소스의 계획이 오면 버릴 기록이다. 기각.
- **새 코드를 첫 표에 더하기**: 첫 표의 잠긴 테스트를 고쳐야 하고(사례 함수가 잠겨 있다) 그 사례는 문서 하나씩 돈다. 기각.
- **JSON 필드 이름 규칙을 매핑에도**: `replace`의 `map` 키가 데이터의 값이라 `secret` 같은 등급 이름이 거절된다. 기각.
- **링크를 모두 따라가기(`EvalSymlinks`)**: 링크의 고리와 바꿔치기를 막는 규칙이 흐려지고 기본 위치까지 넓어진다. 결정 D8과 다르다. 기각.
- **링크 대상의 그룹 읽기 허용**: `mode & 0077` 기준을 느슨하게 하는 일이다. 클러스터에서 그룹 비트가 붙는지는 배포 변경이 확인한다(D8). 기각.

## 결과

- 고친 문서: [서버 운영하기](../../guides/operate-the-server.md) 7절(가져오기, keygen, 링크), [정의의 쓰기](../source-editing.md) 1.4와 1.5, [수집 서비스와 실행](../ingestion-service.md) 10절, REQ-438·440·476(일부 구현으로), 새 REQ-704~707.
- ADR-0076 결정 10의 "링크인 키 파일은 거절" 한계는 명시한 키 파일에서 풀린다. 그룹 읽기 권한의 거절과 다른 한계는 그대로다.
- 한계: 매핑 하나는 소스 하나를 읽는다. 다른 매핑이 읽던 컬럼을 지운 소스는 그 매핑과 한 번에 고치지 못한다(가져오기는 소스 하나와 매핑 하나를 받는다). 소스·매핑의 삭제와 내보내기가 없다. 가져온 정의를 읽는 곳은 아직 없다(`ingest run`은 파일을 읽는다). 값의 검사는 확실한 것만 거절하고, SQL의 `password = 'x'`(빈칸 있음)처럼 확실하지 않은 것은 통과한다. `SetItems`가 실패하는 길(저장소 파일이 열린 뒤 깨짐)은 테스트로 덮지 못했다.
