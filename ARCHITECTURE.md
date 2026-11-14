---
title: 아키텍처 — 단위와 의존 방향
kind: reference
status: active
covers:
  - harness/config/architecture.yaml
  - packages/core/src/kernel/index.ts
  - backend/go.mod
---
# 아키텍처

코드는 **단위**로 나뉜다. 단위는 폴더 하나이고, 무엇을 import해도 되는지는 `harness/config/architecture.yaml`이 정한다. 구조 검사가 이것을 강제한다(TypeScript는 `ARCH*`, Go는 `GOARCH*`, REQ-021, ADR-0019).

## 지금 있는 단위

| 단위 | 언어 | 경로 | 하는 일 | import해도 되는 것 |
|---|---|---|---|---|
| `core.kernel` | TypeScript | `packages/core/src/kernel` | ID, 오류, 시계·난수·ID 발급의 포트, 외부(벽시계, 난수)를 읽는 유일한 진입점, 안정된 해시, 숫자 유틸리티(clamp, 반올림, approach), 한국어 조사와 시간 표기 | 없음 |
| `harness` | TypeScript | `harness/src` | 구조 검사. Go 코드도 읽는다(ADR-0019) | 없음(제품 코드를 import하지 않는다) |
| `core.kernel` | Go | `backend/internal/kernel` | 타입이 있는 ID, 안정된 코드를 가진 오류, 시계·난수·ID 발급의 포트와 구현, 정규 인코딩 기반 SHA-256. 시계와 난수를 읽는 곳은 `entropy.go` 한 파일뿐이다 | 없음 |
| `core.model` | Go | `backend/internal/model` | 타입 등록부(객체·링크·액션 타입, 속성, 특성, 값 타입)의 도메인 타입과 검증, 표준 모델의 JSON 데이터 파일(`go:embed`), 번호를 부여하고 내용 해시를 산출하는 불변 등록부. 오류 코드는 `model_`로 시작하는 자기 목록을 가진다 | `core.kernel` |
| `core.access` | Go | `backend/internal/access` | 역할, 능력(ability), 주체(`Subject`), 인증기(`Authenticator`)와 권한 판정기(`Authorizer`)의 포트, 역할 표. 역할별로 무엇을 할 수 있는지를 정의하는 단일 위치 | `core.kernel` |
| `core.graph` | Go | `backend/internal/graph` | KG의 중심. 객체와 링크의 행(번호로 참조하고 해시에는 영향을 주지 않는 ID), 변경 배치, 등록부 기준 검증, 변경 번호를 부여하며 청크로 나눠 적용하는 적용기, 증분 갱신하는 그래프 해시, 확정 버전, 식별 이력, 리스와 세대. 스토리지 포트(`Catalog`, `Writer`, `Reader`, `Feed`)와 합성 세계의 도구(`Fixture`), 최신 상태만 보유한 구현 `ScratchStore`(테스트 더블)를 함께 가진다. 차수(`1:1`, `N:1`)는 링크가 놓인 바인딩마다 계산한다(ADR-0027) | `core.kernel`, `core.model` |
| `core.query` | Go | `backend/internal/query` | 사전 정의 질의 3종(`find`, `get`, `neighbors`)의 기준 구현. 스토리지 포트의 스냅샷 하나를 읽어 모든 응답에 엔벨로프(`as_of`, `volatile_as_of`, 신선도, 근거, `result_complete`, `hidden`)를 붙이고, 읽기 권한 포트(`ReadFilter`)로 필터링한다. 어댑터를 import하지 않는다. 테스트는 `graph.ScratchStore`에 세계를 구축한다(ADR-0027) | `core.kernel`, `core.model`, `core.graph` |
| `adapter.memstore` | Go | `backend/internal/memstore` | 스토리지 포트의 인메모리 구현. 적합성 테스트(`adapter.graphcheck`)를 모두 통과하는 기준 구현이고 테스트와 개발 모드의 스토리지다. 저널 포트(`Journal`)를 붙여 `Open`하면 `adapter.filejournal`이 받치는 내장 파일 스토어가 된다(처음 써 보기와 소규모용, ADR-0034). 저널이 없으면 프로세스가 끝날 때 사라지는 운영용이 아닌 스토리지다. PostgreSQL 어댑터(`adapter.pgstore`)가 이것을 참고 구현으로 삼았다(ADR-0025) | `core.kernel`, `core.graph` |
| `adapter.filejournal` | Go | `backend/internal/filejournal` | 내장 파일 스토어의 저널(ADR-0034). 데이터 폴더 하나에 추가 전용 로그 세그먼트와 스냅샷(두 세대)을 쓰고, 레코드마다 CRC-32C와 번호를 달아 응답 전에 동기화한다. 시작할 때 최신의 온전한 스냅샷을 읽고 그 뒤의 로그를 재생하며, 끝의 깨진 레코드는 잘라 내고 중간의 손상은 오류로 멈춘다. 디스크는 `Disk` 인터페이스로만 만나고(실제 구현은 `cmd.praxium`이 넘긴다) `adapter.memstore`를 import하지 않는다. 레코드는 바이트이고 그래프를 모른다 | `core.kernel`. 테스트 파일만 `core.graph`, `adapter.graphcheck`, `adapter.memstore` (`test_may_import`) |
| `adapter.pgstore` | Go | `backend/internal/pgstore` | 스토리지 포트의 PostgreSQL 구현(드라이버 `lib/pq`, ADR-0032). 적합성 테스트 31개 사례를 모두 통과한다. 쓰기는 `kg_head` 행을 잠그고 세대, 리스(lease), 엔진 의미 버전, 기대한 `applied_seq`를 차례로 검증한 뒤 행과 `kg_head`를 한 트랜잭션에서 쓴다. 변경 번호는 `kg_head` 행의 카운터다. 모든 기본키와 인덱스는 `kg_id`로 시작하고, 스토리지 접근은 `kg_id`를 키로 하는 라우팅 계층을 거친다(현재는 답이 하나다). 마이그레이션은 직접 만든 실행기와 내장 SQL 파일로 하고, 역할별 문 실행 시간 제한과 잠금 대기 제한을 둔다 | `core.kernel`, `core.model`, `core.graph` |
| `adapter.graphcheck` | Go | `backend/internal/graphcheck` | 스토리지 포트의 적합성 테스트(`Cases()`, 31개 사례. 사례 이름과 순서는 테스트가 고정하고, 사례의 내용은 테스트 잠금이 지문에 포함한다, ADR-0028). 어댑터가 통과해야 하는 요건을 `TB` 인터페이스 기반으로 작성하고, 어댑터의 테스트가 `t.Run`으로 실행한다(ADR-0025) | `core.kernel`, `core.model`, `core.graph`. 테스트 파일만 `adapter.memstore`도 (`test_may_import`) |
| `core.contract` | Go | `backend/internal/contract` | 외부에 공개하는 계약. OpenAPI 3.1 계약을 조각(`openapi/parts`)으로 두고 도구가 병합해(`openapi.json`) `go:embed`로 내장한다. 아직 구현되지 않은 경로의 등록부(`planned/routes.json`), JSON Schema 부분집합 검증기, 문제 코드 목록을 함께 가진다. MCP 툴과 파일 형식은 이후에 추가한다 | `core.kernel` |
| `adapter.devauth` | Go | `backend/internal/devauth` | 개발용 인증기. 고정 토큰 하나와 역할 목록. 운영 모드, 16자 미만 토큰, 정의되지 않은 역할은 거부한다. OIDC로 대체되기 전의 임시 구현이다(ADR-0024, 부채 TD-0010) | `core.kernel`, `core.access` |
| `adapter.http` | Go | `backend/internal/httpapi` | `net/http` 서버 골격. 계약의 경로 32개를 서버로 제공한다(헬스 체크, 목록, 타입 등록부, KG와 객체와 링크의 읽기, 질의 3종의 POST). 읽기 권한 필터링은 역할을 권한 부여로 변환해 `core.query`에 전달한다. 운영 코드에서 `adapter.memstore`를 import하지 않는다(ADR-0027). 엔벨로프, 문제 상세 본문, 커서, ETag, 트레이스 ID, `allowed_actions`를 지원한다. 인증기는 포트로 주입받는다 | `core.kernel`, `core.model`, `core.contract`, `core.access`, `core.graph`, `core.query` |
| `adapter.cli` | Go | `backend/internal/cli` | 같은 바이너리에 포함된 CLI. 공개된 `/v1`만 호출하고 종료 코드는 문제 분류를 따른다. KG를 읽는 명령(`kg`, `object`, `query`)을 가진다 | `core.kernel` |
| `adapter.devload` | Go | `backend/internal/devload` | 개발용 적재기. 생성기의 정답을 적용기로 크기 한도가 있는 변경 배치에 나눠 KG에 적재하고 건수를 보고한다. 개발과 테스트의 편의이지 수집 파이프라인이 아니다(ADR-0027) | `core.kernel`, `core.model`, `core.graph`, `core.worldgen`. 테스트 파일만 `core.query`도 |
| `adapter.readcheck` | Go | `backend/internal/readcheck` | 테스트 전용. 어댑터를 import해야 하는 읽기 테스트(이전 변경 번호와 버전으로 읽기, 적재 후의 속성)와 측정용 벤치마크 | 없음. 테스트 파일만 `core.*`, `adapter.memstore`, `adapter.http`, `adapter.devload` (`test_may_import`) |
| `core.ingest` | Go | `backend/internal/ingest` | 수집 파이프라인의 최소 구현(ADR-0033). 소스 정의와 매핑 정의(JSON 데이터)를 타입 레지스트리로 검증해 계획으로 만들고, 수집, 파싱(CSV, JSON Lines), 원본 레코드의 변경, 정제(`filter`, `fill_empty`, `replace`), 피드, 식별(표준 모델의 식별 키), 링크, 적용기에 전달하는 변경 배치를 거쳐 KG에 넣는다. 행 수 대조와 문제 보고를 내고, 상태(원본 레코드의 구간과 미관측 횟수)는 포트(`StateStore`)로 둔다. 파일도 시계도 직접 읽지 않는다 | `core.kernel`, `core.model`, `core.graph` |
| `adapter.filesource` | Go | `backend/internal/filesource` | 파일 소스 어댑터. 시작점이 전달한 open 함수로 데이터셋마다 파일 하나를 읽어 수집 결과(`Capture`)를 만든다. 쓰기 경로가 없고, 크기 한도를 넘거나 끝까지 읽지 못한 것은 불완전한 수집으로 표시한다 | `core.kernel`, `core.ingest` |
| `adapter.ingestcheck` | Go | `backend/internal/ingestcheck` | 테스트 전용. 수집 슬라이스의 인수 검사: 생성기의 세계에서 형태가 다른 소스 파일을 만들어 파이프라인으로 인메모리 스토리지에 적재하고 개발용 적재기의 KG와 비교한다. 재수집, 한 행 변경, 사라진 행, 불량 행을 확인한다 | 없음. 테스트 파일만 `core.kernel`, `core.model`, `core.graph`, `core.worldgen`, `core.ingest`, `adapter.memstore`, `adapter.devload`, `adapter.filesource` (`test_may_import`) |
| `core.worldgen` | Go | `backend/internal/worldgen` | 합성 세계 생성기. 시드와 인자로 표준 모델만 사용한 데이터센터의 정답(JSON Lines), 소스 뷰(CSV, JSON 페이지, 장치 설정, 고정 폭 테이블), 변경 스트림, 분포 프로파일을 스트리밍으로 생성하고, 정답을 등록부로 검증한다. 파일은 `Store`로 받는다(운영체제에 접근하지 않는다) | `core.kernel`, `core.model` |
| `cmd.praxium` | Go | `backend/cmd/praxium` | 시작점. 역할 이름만 주면 커널을 조립하고 한 줄짜리 구조화 로그를 남긴 뒤 종료한다. `api serve`는 서버를 기동하고, `api routes`, `contract`, 클라이언트 명령(`health`, `types`, `catalog`, `kg`, `object`, `query`)과 개발용 `dev load-truth`, 파일 소스 한 번의 수집 `ingest run`을 가진다. `api serve -mode development`는 기본으로 인메모리 스토리지를 만들고 `-dev-truth`로 세계를 적재한다. `-store postgres -store-dsn-file <파일>`을 주면 PostgreSQL 스토리지를 쓴다(연결 문자열은 파일에서만 읽는다). 운영 모드는 실제 인증기가 없어 거부한다. 환경 변수와 파일, 리스닝, 종료 신호는 `wiring`으로 묶어 테스트에서 대체한다 | `core.*`, `adapter.*` |
| `web.api` | TypeScript | `apps/web/src/api` | 임시 콘솔의 API 호출 계층(ADR-0035). 계약(OpenAPI)에서 생성한 타입과 호출 함수(`generated/`, 손으로 고치지 않는다), 요청을 만들고 오류(`problem+json`)를 `ApiError`로 바꾸는 클라이언트, 의존성 없는 생성기(`generator/`) | 없음 |
| `web.data` | TypeScript | `apps/web/src/data` | 조회와 로딩·오류 상태, 커서 페이지네이션, 주소(해시 라우터), 접속 토큰, 오류를 한국어로 바꾸는 말, 표의 열 고르기 | `web.api` |
| `web.ui` | TypeScript | `apps/web/src/ui` | 프락시움 이름의 컴포넌트와 토큰 파일(`tokens.css`, 색은 여기에만 쓴다). Base UI는 여기에서만 import한다 | 없음 |
| `web.screens` | TypeScript | `apps/web/src/screens` | 임시 화면(접속 설정, KG 선택, 객체 타입, 객체 표, 객체 상세와 이웃). `ui`와 `data`만 쓴다. 테스트 잠금과 요구사항 추적에서 빠진다 | `web.data`, `web.ui` |
| `web.app` | TypeScript | `apps/web/src` | 진입점(`main.tsx`)과 틀(`App.tsx`) | `web.data`, `web.ui`, `web.screens` |
| `web.build` | TypeScript | `apps/web/config` | Vite 설정. 번들을 만든다 | 없음 |
| `web.testsupport` | TypeScript | `apps/web/src/test-support` | 웹 UI 테스트의 렌더와 가짜 API | 없음 |
| `adapter.webui` | Go | `backend/internal/webui` | 빌드한 웹 UI를 `go:embed`로 담는다. 커밋하는 것은 자리표시자 페이지뿐이고 실제 빌드 결과는 `scripts/build-with-web.sh`가 복사한다(ADR-0035). 서버(`adapter.http`)는 파일 시스템 값 하나(`Config.UI`)만 받고 이 패키지를 import하지 않는다 | 없음 |

TypeScript의 `core.kernel`과 Go의 `core.kernel`은 이름만 같고 다른 단위다. 구조 계약에서 Go 단위는 `go:` 절에 따로 적는다.

`core.kernel`은 **화면 쪽의 TypeScript 공통 기반 코드**(한국어 조사, 시간 표기)이므로, 화면이 다른 리포지토리로 분리되어 그 리포지토리가 만들어지면 그쪽으로 옮긴다(부채 TD-0006, ADR-0016). 그때까지 여기에 남고 새 코드는 이것에 의존하지 않는다. 백엔드는 자기 공통 기반 코드를 Go로 따로 가진다(ADR-0006).

## 규칙

아래 첫 목록은 TypeScript 코드(`core.kernel`과 하네스)에 대한 사실이다. 백엔드는 Go로 만들며, 그 규칙은 이 절의 "Go 백엔드의 규칙"과 아래 "정해진 구조"에 적는다(ADR-0006, ADR-0019).

- 다른 단위는 그 단위의 `index.ts`로만 접근한다. 코어의 단위는 `@praxium/core/<단위>` 별칭으로 부른다.
- 코어는 Node를 모른다. 파일, 네트워크, 프로세스는 코어 밖(서버의 어댑터)에 두고, 코어는 포트(인터페이스)만 안다.
- 시계와 난수는 `kernel`의 `Clock`과 `Rng`를 주입받는다. 제품 코드에서 직접 읽을 수 있는 곳은 `kernel`의 한 파일(`packages/core/src/kernel/entropy.ts`)뿐이다(REQ-001, `ARCH052`). 하네스는 제품 코드가 아니라서 이 규칙에서 빠진다.
- 외부 패키지는 구조 계약의 `third_party`에 적힌 단위에서만 사용한다. 새 패키지는 결정 기록이 있어야 들어온다.
- 파일 500줄, 함수 80줄, 복잡도 12, 매개변수 6개가 한도다(`DEBT*`).

## Go 백엔드의 규칙 (ADR-0019)

Go 코드는 `backend/` 한 모듈에 있다. 단위는 `internal/<단위>` 패키지 하나이고, 시작점은 `cmd/praxium` 하나다. 구조 검사가 읽는 계약은 `harness/config/architecture.yaml`의 `go:` 절이다.

- 단위는 폴더 하나의 패키지이고, 다른 단위는 그 패키지의 루트로만 접근한다. 단위 안으로 들어가는 import는 `GOARCH011`, 허용되지 않은 방향의 import는 `GOARCH010`이다. 단위 사이의 순환은 `GOARCH030`이다.
- 단위마다 패키지 설명(`// Package <이름> ...`)이 있어야 한다(`GOARCH041`). 어느 단위에도 속하지 않은 Go 파일은 `GOARCH001`이다.
- 시계, 난수, 환경 변수는 지정된 파일(`backend/internal/kernel/entropy.go`)에서만 읽는다(`time.Now`, `time.Since`, `time.Until`, `os.Getenv`, `os.LookupEnv`, `os.Environ`, `math/rand`, `crypto/rand`). 나머지는 `kernel.Clock`과 `kernel.Rng`를 주입받는다(`GOARCH052`, REQ-001). 테스트 파일도 같다.
- 코어는 운영체제와 네트워크를 모른다. `os`, `net/http` 같은 패키지는 구조 계약의 `restricted_stdlib`에 적힌 단위만 import한다(`GOARCH021`). 항목이 `.go`로 끝나면 단위가 아니라 그 파일 하나다: `core.kernel`에서 `os`를 import할 수 있는 곳은 환경 변수를 읽는 `entropy.go` 한 파일이다(ADR-0024가 진입점을 그 파일로 정했고, ADR-0028이 단위 전체에서 파일 하나로 좁혔다). 프로세스를 종료하는 `os.Exit`와 `log.Fatal*`은 시작점에서만 쓴다(`GOARCH050`). `func init`, 점 import, 시작점 밖의 빈 import는 쓰지 않는다(`GOARCH053`, `GOARCH051`).
- 단위가 import할 수 있는 단위는 두 목록으로 적는다. `may_import`는 제품 코드와 테스트 파일이 함께 쓰고, `test_may_import`는 `_test.go` 파일만 추가로 쓴다. 제품 코드가 테스트 전용 목록의 단위를 import하면 `GOARCH010`이고, 두 목록에 같은 이름이 있으면 `GOARCH005`다. 적합성 테스트 단위(`adapter.graphcheck`)가 기준 구현(`adapter.memstore`)을 import하는 것은 테스트 파일에서만 허용된다(ADR-0028).
- import 경로는 보이는 그대로 적는다. `"o\x73"` 같은 이스케이프나 경로에 쓰지 않는 문자는 `GOARCH054`다. 테스트 파일에는 빌드 제약(`//go:build`, `// +build`, `_linux_test.go` 같은 OS·아키텍처 접미사)을 달 수 없다: 일부 환경에서만 실행되는 테스트는 잠금과 추적의 대상이지만 실제로는 실행되지 않기 때문이다(`GOARCH055`). 컴파일된 실행 파일과 테스트 바이너리는 커밋하지 않으며 `.gitignore`가 차단한다(`GOARCH056`).
- 테스트가 의존하는 비테스트 코드(적합성 테스트의 사례 목록, `graph`의 테스트용 `Fixture`와 `ScratchStore`)는 구조 계약의 `go.locked_support`에 적는다. 그 코드는 그것을 호출하는 테스트의 지문에 포함되므로, 사례를 삭제하거나 단언을 약화하면 그 테스트의 잠금이 깨진다(`TLOCK001`).
- 외부 모듈은 구조 계약의 `go.third_party`에 적힌 단위에서만 사용한다(`GOARCH020`). 모듈을 추가하는 변경은 결정 기록과 아래 표를 함께 수정한다(`GODEP*`).
- 파일 500줄, 함수 80줄, 복잡도 12, 매개변수 6개가 한도다. 복잡도는 `if`, `for`, `case`, `&&`, `||`의 개수를 센 값이다(`GODEBT020`~`GODEBT023`).
- `//nolint`, `//lint:ignore`, `//go:build ignore`, 건너뛴 테스트(`t.Skip`)는 이유와 `TD-NNNN` 또는 `ADR-NNNN`이 있어야 한다(`GOIGN*`).
- 단위의 코드가 있으면 테스트가 있어야 하고(`GOTEST002`), `gofmt`와 `go vet`이 깨끗해야 하고(`GOTOOL001`, `GOTOOL002`), 테스트는 단위마다 문장 커버리지 하한을 지킨다(`GOCOV001`, `GOCOV002`). 하한은 `harness/config/coverage-floor.json`의 `go:<단위>` 항목이다.
- Go가 설치돼 있어야 한다. 없으면 `GOTOOL000`이다.

## 정해진 구조 (ADR-0006). 코드가 생길 때 구조 계약에 등록한다

원칙은 "하나만 쓴다"가 아니라 "용도에 맞는 것을 쓴다"이고, 지킬 것은 **단일 진실 공급원은 하나**와 **의미의 구현은 하나**다. 백엔드의 중심(정의, 실행기, API)은 Go, 구조 검사는 TypeScript다. 이 리포지토리는 백엔드다. 화면은 다른 리포지토리에서 TypeScript로 만든다(ADR-0016). 질의 엔진도 Go로 직접 만든다(기존 그래프 엔진은 쓰지 않는다. ADR-0006, ADR-0010). 수집 엔벨로프의 클라이언트 라이브러리는 여러 언어(우선 Go, Python, TypeScript)로 제공한다. 이 절은 정해진 방향이다. 지금 코드가 있는 것은 `core.kernel`, `core.model`, `core.worldgen`(합성 세계 생성기, ADR-0021), `core.graph`, `adapter.memstore`, `adapter.graphcheck`(ADR-0025), `core.query`(ADR-0026), `core.contract`, 시작점이다. 나머지 단위는 코드가 생기는 변경에서 `harness/config/architecture.yaml`에 등록하고, 폴더 위치도 구현하는 변경에서 정한다. Go용 구조 검사는 ADR-0019가 만들었다.

### 백엔드(Go)의 단위 방향

```
core.kernel
  └─ core.model        타입 등록부, 링크 의미, 근거 등급, 식별 키(속성, 문맥, 집합), 표준 모델
       ├─ core.worldgen     합성 세계 생성기: 정답, 소스 뷰, 변경 스트림, 분포 프로파일 (kernel과 model만 import한다)
       ├─ core.definitions  정의의 저장, 브랜치, 제안, 병합
       ├─ core.ingest       소스 설정, 프리셋, 원본 레코드, 정제, 매핑, 식별, 링크 정의 (어댑터 포트)
       ├─ core.graph        KG 버전, 후보와 차이, 적용과 안전장치 (스토리지 포트)
       │    └─ core.query       사전 정의 질의, 영향 범위의 의미와 기준 구현 (엔진 포트, 프로젝션 형식)
       ├─ core.observe      품질, 헬스 상태, 목표, 알림
       ├─ core.competency   역량 질문과 충족률
       └─ core.schedule     동기화 실행과 일정
core.access     SSO와 권한 부여, 감사 (kernel과 model만 import한다. 다른 단위가 이것을 import한다)
core.contract   외부에 공개하는 계약: HTTP 경로와 스키마, MCP 툴의 스키마, 파일 형식(프로젝션, 수집 엔벨로프, 정의 내보내기)
바깥 층
  ├─ 어댑터들   프로토콜 어댑터, PostgreSQL 스토리지, 시크릿 제공자, 로그인, 영향 범위 엔진, 알림
  ├─ HTTP, MCP  계약을 서버로 제공하는 층
  └─ 시작점     조립. 역할 4종(API, 워커, 질의 엔진, 수집기) 가운데 하나를 골라 기동한다
```

- 엔드투엔드(E2E) 프로세스의 단위(업무, 건, 위임)는 그 일을 구체화할 때 추가한다.
- 화살표는 "import할 수 있다"가 아니라 "위 단위가 아래 단위를 import한다"는 뜻이다. 아래 단위는 위 단위에 의존하지 않는다.

### 화면은 다른 리포지토리다 (ADR-0016)

- 제품의 화면 단위는 없다. 제품의 화면은 다른 리포지토리에서 계약(OpenAPI)으로만 통신하고, 계약에서 클라이언트를 생성해 쓴다. 백엔드의 코드를 import하지 않는다.
- 계약은 이 리포지토리가 소유하고 개방형 형식(OpenAPI, JSON Schema)으로 공개한다. 서버가 계약에 맞는지, 호환성을 깨는 변경이 없는지를 이 리포지토리의 검사가 확인한다.
- 이 리포지토리가 제공하는 것: 실행 파일(역할 4종), REST API의 계약, MCP 서버, 수집 엔벨로프의 클라이언트 라이브러리, CLI. CLI는 API를 그대로 호출해서 화면 없이도 모든 기능을 테스트하고 운영할 수 있게 한다.
- 예외 하나: 클로즈드 베타까지 삭제할 수 있는 임시 콘솔을 `apps/web`에 둔다(React와 Vite의 정적 단일 페이지 앱. ADR-0035가 ADR-0016의 9번을 고쳤다). 공개된 `/v1`만 호출하고 콘솔 전용 경로는 없다. 빌드 결과는 `adapter.webui`로 실행 파일에 담겨 `praxium api serve`가 `/v1` 밖에서 낸다. 백엔드는 `apps/web` 없이 빌드되고 테스트된다(자리표시자만 커밋. `WEB030`~`WEB032`). 화면 계층은 위로만 의존하고 Base UI는 `web.ui`에서만 import한다(`ARCH020`). 색 리터럴은 토큰 파일 밖에서 실패하고(`WEB020`, `WEB021`), 생성한 API 코드가 계약과 다르면 실패한다(`WEB010`~`WEB013`). 제품의 화면과 UX·UI 설계는 프런트엔드 리포지토리(아직 없다)의 일이다. 이 리포지토리의 알파 기준은 "모든 기능이 API와 CLI로 동작한다"이다. 삭제 조건은 TD-0007이다.

### 클린 아키텍처의 원칙 (Go의 관례로)

- 단위는 패키지 하나다. 다른 단위는 그 패키지의 공개 인터페이스로만 접근한다.
- 무엇을 import해도 되는지와 어떤 외부 모듈을 사용하는지는 단위마다 구조 계약에 적고, 검사가 강제한다.
- 포트(인터페이스)는 사용하는 쪽 코어 단위에 둔다. 어댑터는 바깥 층에 둔다. 소스에 접속하는 어댑터의 포트에는 쓰기 메서드가 없다(ADR-0008).
- 조립은 시작점 한 곳에서만 한다. 어댑터의 구체 객체를 생성하는 곳도 거기뿐이다.
- 코어는 운영체제와 네트워크를 모른다. 시계와 난수는 주입받고, 직접 읽는 곳은 공통 기반 코드의 한 곳뿐이다(REQ-001).
- 순환 의존을 두지 않고, 같은 층의 형제 단위끼리 직접 import하지 않는다.
- 새 외부 모듈은 결정 기록이 있어야 들어온다.
- 현실 시스템에는 읽기만 한다. 실제 툴을 호출하는 코드는 어느 층에도 두지 않는다(ADR-0001, ADR-0005).

### 역할 4종

실행 파일 하나가 역할을 선택해 기동한다. 개발과 알파에서는 한 프로세스에 함께 기동할 수 있다.

| 역할 | 하는 일 | 복수 인스턴스 |
|---|---|---|
| API | 계약의 경로와 MCP. 상태를 보유하지 않는다 | 가능 |
| 워커 | 수집부터 적용까지와 일정. 소스별 동시 실행 상한을 지킨다 | 가능 |
| 질의 엔진 | 프로젝션을 메모리에 올려 영향 범위와 경로를 응답한다. 프로젝션 파일과 변경 시퀀스만 읽는다 | 가능 |
| 수집기 | 워커의 수집 역할을 소스가 있는 망에 별도로 기동한 것 | 망마다 |

질의 엔진은 Go로 직접 만들고, 프로젝션 파일과 변경 시퀀스가 엔진의 경계다.

### 스토리지 구성

- 정본은 PostgreSQL에 둔다. 나머지는 거기서 파생한 것이고 다시 만들 수 있다.
- 내부 변경 로그는 포트 뒤에 어댑터 둘(PostgreSQL, 메시지 로그 스토리지)을 모두 정식으로 둔다.
- 오래된 데이터는 개방형 형식의 파일로 오브젝트 스토리지에 아카이브할 수 있다. 분석용 스토리지는 파생한 것이다.
- 스트림 처리 엔진으로 실행기를 대체하지 않는다. 의미의 구현이 둘이 되어 결정성을 잃기 때문이다.
- 어느 제품을 쓸지는 구현하는 변경의 결정 기록에서 정한다.

### KG는 여럿이고 정의의 메인 브랜치는 하나다 (ADR-0009)
KG는 계약의 자원이다(`/v1/kgs/{kg}/...`). 운영 KG(`main`), 후보 KG, 테스트 KG, 시스템 KG가 있고, 정의의 메인 브랜치는 배포에 하나다. KG는 그 메인 브랜치의 어느 정의 세트를 사용하는지 가리킨다(`applied_set_no`). 영향 범위는 KG 하나 안에서만 계산된다. KG 분할은 쓰기 한계를 해소하는 수단이 아니며, 서로 의존하지 않는 자산 그룹만 다른 KG가 된다. 한 KG에 수천만 개를 담고 설계 여유는 수억 개까지다.

## 의존성 표 (ADR-0006, REQ-239)
라이브러리를 추가하는 변경은 이 표를 함께 수정한다. 허용 라이선스만 도입한다.

| 구분 | 무엇 | 없을 때 |
|---|---|---|
| 배포의 필수 | PostgreSQL | 없으면 기동하지 않는다 |
| 배포의 필수 | OIDC 제공자(사내 SSO) | 로그인이 없다. 서비스 계정만 쓴다 |
| 배포의 필수 | 마스터 키 보관소(키 관리 서비스, 하드웨어 보안 모듈, 또는 배포가 주는 키 파일) | 제품에 내장된 시크릿 스토리지를 쓰지 못한다 |
| 선택 | 메시지 로그 스토리지(Kafka 규약. 조직이 이미 운영하는 것) | PostgreSQL 어댑터를 쓴다 |
| 선택 | 외부 시크릿 스토리지 | 제품에 내장된 엔벨로프 암호화 스토리지를 쓴다 |
| 선택 | 파일 보관(S3 규약) | 보존 기한 안에서 PostgreSQL에 둔다 |
| 선택 | 개방형 형식으로 관측 데이터를 수신하는 곳 | 제품 안의 신호와 `diagnose`만 쓴다 |
| 도입하는 라이브러리 | 질의 엔진의 핵심과 파서는 표준 라이브러리로 직접 구현한다. 도입한 Go 모듈은 아래 표에 있다(PostgreSQL 드라이버 하나) | |
| 도입하는 라이브러리 | YAML 파싱(구현하는 변경이 정한다. 사용하는 부분집합을 직접 파싱하거나 허용 라이선스의 라이브러리 하나) | 부분집합을 직접 파싱한다 |

기존 그래프 엔진은 도입하지 않는다(허용 라이선스, 메모리 상주, 증분 갱신, 과거 버전, 제품의 의미 체계를 함께 충족하는 것이 없다. ADR-0006).

### 도입한 웹 UI 패키지 (REQ-277, ADR-0035)

`apps/web/package.json`의 의존성은 허용 목록(`harness/config/web.yaml`)에 있어야 하고(`WEB001`), 실행 파일에 들어가는 것은 라이선스 허용 목록(`harness/config/licenses.yaml`)에 있어야 한다. 전이 의존은 `pnpm-lock.yaml`에서 끝까지 센 수다.

| 패키지 | 버전 | 라이선스 | 전이 의존 | 이유 | 쓰는 단위 |
|---|---|---|---|---|---|
| `react` | 19.3.1 | MIT | 0 | 화면을 선언적으로 그린다. 직접 만들면 갱신과 포커스 관리를 다시 짜야 한다 | `web.ui`, `web.data`, `web.screens`, `web.app` |
| `react-dom` | 19.3.1 | MIT | 2 | React를 브라우저에 그린다 | `web.app` |
| `@base-ui/react` | 1.9.0 | MIT | 13 | 대화상자, 탭, 선택 상자의 키보드 조작과 접근성 동작. 스타일이 없는 부품이라 토큰만으로 꾸민다 | `web.ui` |
| `tailwindcss`, `@tailwindcss/vite` | 4.3.5 | MIT | 0, 108 | 토큰 하나를 스타일의 전부로 만들고 쓰지 않는 스타일을 뺀다(개발용) | `web.build` |
| `vite` | 7.3.8 | MIT | 83 | 정적 번들을 만든다. 테스트 도구(`vitest`)가 이미 쓰는 같은 버전이다(개발용) | `web.build` |
| `happy-dom` | 21.0.4 | MIT | 8 | 훅과 컴포넌트 테스트용 DOM(개발용) | 테스트 파일 |
| `@types/react`, `@types/react-dom` | 19.3.0 | MIT | 1, 2 | 타입 검사(개발용) | 없음 |

### 도입한 Go 모듈 (REQ-239, ADR-0019)

`backend/go.mod`의 `require`는 간접 의존까지 모두 아래 표에 항목마다 한 행씩 있어야 한다. 라이선스는 허용 목록(`harness/config/licenses.yaml`)에 있어야 하고, 직접 구현하지 않는 이유가 적혀 있어야 한다. 이 표와 `go.mod`가 다르면 `GODEP010`, `GODEP011`, `GODEP012`가 실패한다. `replace`와 `exclude`는 쓰지 않는다(`GODEP020`).

| 모듈 | 라이선스 | 이유 | 쓰는 단위 |
|---|---|---|---|
| `github.com/lib/pq` | MIT | PostgreSQL의 와이어 프로토콜과 인증(SCRAM-SHA-256 등)을 직접 구현하면 오래 안전하게 유지하기 어렵다. 순수 Go이고 다른 모듈에 의존하지 않는다(전이 의존 없음, ADR-0032) | `adapter.pgstore` |
