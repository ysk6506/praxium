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

| 단위 | 언어 | 자리 | 하는 일 | import해도 되는 것 |
|---|---|---|---|---|
| `core.kernel` | TypeScript | `packages/core/src/kernel` | ID, 오류, 시계·난수·ID 발급의 포트, 바깥(벽시계, 난수)을 읽는 하나뿐인 입구, 안정된 해시, 숫자 도우미(자르기, 반올림, 다가가기), 한국어 조사와 시간 표기 | 없음 |
| `harness` | TypeScript | `harness/src` | 구조 검사. Go 코드도 읽는다(ADR-0019) | 없음(제품 코드를 import하지 않는다) |
| `core.kernel` | Go | `backend/internal/kernel` | 타입 있는 ID, 안정된 코드를 가진 오류, 시계·난수·ID 발급의 포트와 구현, 정규 인코딩 위의 SHA-256. 시계와 난수를 읽는 곳은 `entropy.go` 한 파일뿐이다 | 없음 |
| `core.model` | Go | `backend/internal/model` | 타입 등록부(객체·링크·액션 타입, 속성, 특성, 값 타입)의 도메인 타입과 검증, 표준 모델의 JSON 데이터 파일(`go:embed`), 번호를 매기고 내용 해시를 내는 불변 등록부. 오류 코드는 `model_`로 시작하는 자기 목록을 가진다 | `core.kernel` |
| `core.access` | Go | `backend/internal/access` | 역할, 능력(ability), 주체(`Subject`), 인증기(`Authenticator`)와 권한 판정기(`Authorizer`)의 포트, 역할표. 어느 역할이 무엇을 할 수 있는지의 한 곳 | `core.kernel` |
| `core.graph` | Go | `backend/internal/graph` | KG의 중심. 객체와 링크의 행(번호로 가리키고 해시에는 바뀌지 않는 ID), 변경 묶음, 등록부에 대한 검증, 변경 번호를 매기며 쪽으로 나눠 적용하는 적용기, 이어서 고치는 그래프 해시, 봉한 버전, 식별 이력, 점유와 세대. 저장소 포트(`Catalog`, `Writer`, `Reader`, `Feed`)와 지어낸 세계의 도구(`Fixture`), 최신만 쥔 구현 `ScratchStore`(시험의 대역)를 함께 가진다. 차수(`1:1`, `N:1`)는 링크가 놓인 바인딩마다 센다(ADR-0027) | `core.kernel`, `core.model` |
| `core.query` | Go | `backend/internal/query` | 닫힌 질의의 첫 셋(`find`, `get`, `neighbors`)의 기준 구현. 저장소 포트의 스냅숏 하나를 읽어 모든 답에 봉투(`as_of`, `volatile_as_of`, 신선도, 근거, `result_complete`, `hidden`)를 달고, 읽기 권한 포트(`ReadFilter`)로 거른다. 어댑터를 읽지 않는다. 시험은 `graph.ScratchStore`에 세계를 짓는다(ADR-0027) | `core.kernel`, `core.model`, `core.graph` |
| `adapter.memstore` | Go | `backend/internal/memstore` | 저장소 포트의 메모리 구현. 적합 시험(`adapter.graphcheck`)을 모두 통과하는 기준 구현이고 시험과 개발 모드의 저장소다. 운영용이 아니다. 뒤에 올 PostgreSQL 어댑터가 이것을 본보기로 삼는다(ADR-0025) | `core.kernel`, `core.graph` |
| `adapter.graphcheck` | Go | `backend/internal/graphcheck` | 저장소 포트의 적합 시험(`Cases()`, 31사례. 사례 이름과 차례는 시험이 고정하고, 사례의 내용은 시험 잠금이 지문에 넣는다, ADR-0028). 어댑터가 통과해야 하는 일을 `TB` 인터페이스 위에 적고, 어댑터의 시험이 `t.Run`으로 돌린다(ADR-0025) | `core.kernel`, `core.model`, `core.graph`. 시험 파일만 `adapter.memstore`도 (`test_may_import`) |
| `core.contract` | Go | `backend/internal/contract` | 밖으로 내는 모양. OpenAPI 3.1 계약을 조각(`openapi/parts`)으로 두고 기계가 합쳐(`openapi.json`) `go:embed`로 내장한다. 아직 없는 길의 등록부(`planned/routes.json`), JSON Schema 부분집합 검증기, 문제 코드 목록을 함께 가진다. MCP 툴과 파일 형식은 이후 | `core.kernel` |
| `adapter.devauth` | Go | `backend/internal/devauth` | 개발용 인증기. 고정 토큰 하나와 역할 목록. 운영 모드, 16자 미만 토큰, 모르는 역할은 거부한다. OIDC로 바뀌기 전의 임시이다(ADR-0024, 부채 TD-0010) | `core.kernel`, `core.access` |
| `adapter.http` | Go | `backend/internal/httpapi` | `net/http` 서버 골격. 계약의 길 32개를 서버로 낸다(상태 확인, 목록, 타입 등록부, KG와 객체와 링크의 읽기, 질의 셋의 POST). 읽기 거름은 역할을 부여로 옮겨 `core.query`에 건넨다. 운영 코드에서 `adapter.memstore`를 읽지 않는다(ADR-0027). 봉투, 문제 본문, 커서, ETag, 추적 ID, `allowed_actions`. 인증기는 포트로 받는다 | `core.kernel`, `core.model`, `core.contract`, `core.access`, `core.graph`, `core.query` |
| `adapter.cli` | Go | `backend/internal/cli` | 같은 바이너리에 든 명령줄 클라이언트. 공개된 `/v1`만 부르고 종료 코드는 문제의 묶음을 따른다. KG를 읽는 명령(`kg`, `object`, `query`)을 가진다 | `core.kernel` |
| `adapter.devload` | Go | `backend/internal/devload` | 개발용 적재기. 생성기의 정답을 적용기로 한도가 있는 변경 묶음에 나눠 KG에 싣고 수를 알린다. 개발과 시험의 편의이지 수집 파이프라인이 아니다(ADR-0027) | `core.kernel`, `core.model`, `core.graph`, `core.worldgen`. 시험 파일만 `core.query`도 |
| `adapter.readcheck` | Go | `backend/internal/readcheck` | 시험 전용. 어댑터를 읽어야 하는 읽기의 시험(옛 변경 번호와 버전으로 읽기, 적재 뒤의 속성)과 측정의 벤치마크 | 없음. 시험 파일만 `core.*`, `adapter.memstore`, `adapter.http`, `adapter.devload` (`test_may_import`) |
| `core.worldgen` | Go | `backend/internal/worldgen` | 지어낸 세계 생성기. 씨앗과 인자로 표준 모델만 쓴 데이터센터의 정답(JSON Lines)과 소스 보기(CSV, JSON 쪽, 장치 설정, 고정 폭 표)와 변경 스트림과 모양 카드를 흘려 내고, 정답을 등록부로 검사한다. 파일은 `Store`로 받는다(운영체제를 읽지 않는다) | `core.kernel`, `core.model` |
| `cmd.praxium` | Go | `backend/cmd/praxium` | 시작점. 역할 이름만 주면 커널을 엮고 한 줄 구조 로그를 남긴 뒤 끝난다. `api serve`는 서버를 띄우고, `api routes`, `contract`, 클라이언트 명령(`health`, `types`, `catalog`, `kg`, `object`, `query`)과 개발용 `dev load-truth`를 가진다. `api serve -mode development`는 메모리 저장소를 만들고 `-dev-truth`로 세계를 싣는다. 운영 모드는 진짜 인증기와 운영 저장소가 없어 거부한다. 환경과 파일, 청취, 정지 신호는 `wiring`으로 묶어 시험이 바꿔 끼운다 | `core.*`, `adapter.*` |

TypeScript의 `core.kernel`과 Go의 `core.kernel`은 이름만 같고 다른 단위다. 구조 계약에서 Go 단위는 `go:` 절에 따로 적는다.

`core.kernel`은 **화면 쪽의 TypeScript 바탕 부품**(한국어 조사, 시간 표기)이라, 화면이 다른 저장소로 나뉘어 그 저장소가 서면 그리로 옮긴다(부채 TD-0006, ADR-0016). 그때까지 여기에 남고 새 코드가 기대지 않는다. 백엔드는 자기 바탕 부품을 Go로 갖는다(ADR-0006).

## 규칙

아래 첫 목록은 TypeScript 코드(`core.kernel`과 하네스)에 대한 사실이다. 백엔드는 Go로 만들고, 그 규칙은 이 절의 "Go 백엔드의 규칙"과 아래 "정해진 구조"에 적는다(ADR-0006, ADR-0019).

- 다른 단위는 그 단위의 `index.ts`로만 닿는다. 코어의 단위는 `@praxium/core/<단위>` 별칭으로 부른다.
- 코어는 Node를 모른다. 파일, 네트워크, 프로세스는 코어 밖(서버의 어댑터)에 두고, 코어는 포트(인터페이스)만 안다.
- 시계와 난수는 `kernel`의 `Clock`과 `Rng`를 주입받는다. 제품 코드에서 직접 읽을 수 있는 곳은 `kernel`의 한 파일(`packages/core/src/kernel/entropy.ts`)뿐이다(REQ-001, `ARCH052`). 하네스는 제품 코드가 아니라서 이 규칙에서 빠진다.
- 외부 패키지는 구조 계약의 `third_party`에 적힌 단위에서만 쓴다. 새 패키지는 결정 기록이 있어야 들어온다.
- 파일 500줄, 함수 80줄, 복잡도 12, 매개변수 6개가 한도다(`DEBT*`).

## Go 백엔드의 규칙 (ADR-0019)

Go 코드는 `backend/` 한 모듈에 있다. 단위는 `internal/<단위>` 패키지 하나이고, 시작점은 `cmd/praxium` 하나다. 구조 검사가 읽는 계약은 `harness/config/architecture.yaml`의 `go:` 절이다.

- 단위는 폴더 하나의 패키지이고, 다른 단위는 그 패키지의 루트로만 닿는다. 단위 안으로 들어가는 import는 `GOARCH011`, 허용되지 않은 방향의 import는 `GOARCH010`이다. 단위 사이의 순환은 `GOARCH030`이다.
- 단위마다 패키지 설명(`// Package <이름> ...`)이 있어야 한다(`GOARCH041`). 어느 단위에도 들지 않은 Go 파일은 `GOARCH001`이다.
- 시계, 난수, 환경 변수는 지정된 파일(`backend/internal/kernel/entropy.go`)에서만 읽는다(`time.Now`, `time.Since`, `time.Until`, `os.Getenv`, `os.LookupEnv`, `os.Environ`, `math/rand`, `crypto/rand`). 나머지는 `kernel.Clock`과 `kernel.Rng`를 주입받는다(`GOARCH052`, REQ-001). 시험 파일도 같다.
- 코어는 운영체제와 네트워크를 모른다. `os`, `net/http` 같은 패키지는 구조 계약의 `restricted_stdlib`에 적힌 단위만 import한다(`GOARCH021`). 항목이 `.go`로 끝나면 단위가 아니라 그 파일 하나다: `core.kernel`에서 `os`를 import할 수 있는 곳은 환경 변수를 읽는 `entropy.go` 한 파일이다(ADR-0024가 입구를 그 파일로 정했고, ADR-0028이 단위 전체에서 파일 하나로 좁혔다). 프로세스를 끝내는 `os.Exit`와 `log.Fatal*`은 시작점에서만 쓴다(`GOARCH050`). `func init`, 점 import, 시작점 밖의 빈 import는 쓰지 않는다(`GOARCH053`, `GOARCH051`).
- 단위가 import할 수 있는 단위는 두 목록으로 적는다. `may_import`는 제품 코드와 시험 파일이 함께 쓰고, `test_may_import`는 `_test.go` 파일만 더 쓴다. 제품 코드가 시험 전용 목록의 단위를 import하면 `GOARCH010`이고, 두 목록에 같은 이름이 있으면 `GOARCH005`다. 적합 시험 단위(`adapter.graphcheck`)가 기준 구현(`adapter.memstore`)을 아는 것은 시험 파일에서만이다(ADR-0028).
- import 경로는 보이는 대로 쓴다. `"o\x73"` 같은 이스케이프나 경로에 쓰지 않는 문자는 `GOARCH054`다. 시험 파일에는 빌드 제약(`//go:build`, `// +build`, `_linux_test.go` 같은 OS·아키텍처 접미사)을 달 수 없다: 일부 환경에서만 도는 시험은 잠금과 추적에는 잡히고 실행은 안 된다(`GOARCH055`). 컴파일된 실행 파일과 시험 바이너리는 커밋하지 않고 `.gitignore`가 막는다(`GOARCH056`).
- 시험이 기대는 비시험 코드(적합 시험의 사례표, `graph`의 시험용 `Fixture`와 `ScratchStore`)는 구조 계약의 `go.locked_support`에 적는다. 그 코드는 그것을 부르는 시험의 지문에 들어가서, 사례를 지우거나 단언을 약하게 하면 그 시험의 잠금이 깨진다(`TLOCK001`).
- 외부 모듈은 구조 계약의 `go.third_party`에 적힌 단위에서만 쓴다(`GOARCH020`). 모듈을 더하는 변경은 결정 기록과 아래 표를 함께 고친다(`GODEP*`).
- 파일 500줄, 함수 80줄, 복잡도 12, 매개변수 6개가 한도다. 복잡도는 `if`, `for`, `case`, `&&`, `||`를 센 값이다(`GODEBT020`~`GODEBT023`).
- `//nolint`, `//lint:ignore`, `//go:build ignore`, 건너뛴 시험(`t.Skip`)은 이유와 `TD-NNNN` 또는 `ADR-NNNN`이 있어야 한다(`GOIGN*`).
- 단위의 코드가 있으면 시험이 있어야 하고(`GOTEST002`), `gofmt`와 `go vet`이 깨끗해야 하고(`GOTOOL001`, `GOTOOL002`), 시험은 단위마다 문장 커버리지 하한을 지킨다(`GOCOV001`, `GOCOV002`). 하한은 `harness/config/coverage-floor.json`의 `go:<단위>` 항목이다.
- Go가 설치돼 있어야 한다. 없으면 `GOTOOL000`이다.

## 정해진 구조 (ADR-0006). 코드가 생길 때 구조 계약에 등록한다

원칙은 "하나만 쓴다"가 아니라 "자리에 맞는 것을 쓴다"이고, 지킬 것은 **진실의 자리는 하나**와 **뜻의 구현은 하나**다. 백엔드의 중심(정의, 실행기, API)은 Go, 구조 검사는 TypeScript다. 이 저장소는 백엔드다. 화면은 다른 저장소에서 TypeScript로 만든다(ADR-0016). 질의 엔진도 Go로 직접 만든다(기성 그래프 엔진은 쓰지 않는다. ADR-0006, ADR-0010). 수집 봉투의 클라이언트 라이브러리는 여러 언어(Go, Python, TypeScript부터)로 낸다. 이 절은 정해진 방향이다. 지금 코드가 있는 것은 `core.kernel`, `core.model`, `core.worldgen`(지어낸 세계 생성기, ADR-0021), `core.graph`, `adapter.memstore`, `adapter.graphcheck`(ADR-0025), `core.query`(ADR-0026), `core.contract`, 시작점이다. 나머지 단위는 코드가 생기는 변경에서 `harness/config/architecture.yaml`에 등록하고, 폴더의 자리도 구현하는 변경에서 정한다. Go용 구조 검사는 ADR-0019가 만들었다.

### 백엔드(Go)의 단위 방향

```
core.kernel
  └─ core.model        타입 등록부, 링크의 뜻, 근거 등급, 식별 키(속성, 문맥, 집합), 표준 모델
       ├─ core.worldgen     지어낸 세계 생성기: 정답, 소스 보기, 변경 스트림, 모양 카드 (kernel과 model만 읽는다)
       ├─ core.definitions  정의의 저장, 브랜치, 제안, 병합
       ├─ core.ingest       소스 설정, 프리셋, 원본 기록, 정제, 매핑, 식별, 링크 정의 (어댑터 포트)
       ├─ core.graph        KG 버전, 후보와 차이, 적용과 안전장치 (저장소 포트)
       │    └─ core.query       닫힌 질의, 영향 범위의 뜻과 기준 구현 (엔진 포트, 투영 형식)
       ├─ core.observe      품질, 건강, 목표, 알림
       ├─ core.competency   역량 질문과 충족률
       └─ core.schedule     동기화 실행과 일정
core.access     SSO와 권한 부여, 감사 (kernel과 model만 읽는다. 다른 단위가 이것을 읽는다)
core.contract   밖으로 내는 모양: HTTP 길과 스키마, MCP 툴의 모양, 파일 형식(투영, 수집 봉투, 정의 내보내기)
바깥 층
  ├─ 어댑터들   프로토콜 어댑터, PostgreSQL 저장소, 비밀 값 제공자, 로그인, 영향 범위 엔진, 알림
  ├─ HTTP, MCP  계약을 서버로 내는 층
  └─ 시작점     조립. 역할 넷(API, 일꾼, 질의 엔진, 수집기) 가운데 하나를 골라 뜬다
```

- E2E 프로세스의 단위(업무, 건, 맡김)는 그 일을 구체화할 때 더한다.
- 화살표는 "읽을 수 있다"가 아니라 "위가 아래를 읽는다"는 뜻이다. 아래 단위는 위 단위를 모른다.

### 화면은 다른 저장소다 (ADR-0016)

- 이 저장소에 화면 단위는 없다. 화면은 다른 저장소에서 계약(OpenAPI)으로만 대화하고, 계약에서 클라이언트를 만들어 쓴다. 백엔드의 코드를 import하지 않는다.
- 계약은 이 저장소가 쥐고 열린 형식(OpenAPI, JSON Schema)으로 낸다. 서버가 계약에 맞는지, 깨는 변경이 없는지를 이 저장소의 검사가 본다.
- 이 저장소가 내는 것: 실행 파일(역할 넷), REST API의 계약, MCP 서버, 수집 봉투의 클라이언트 라이브러리, 명령줄 클라이언트. 명령줄 클라이언트는 API를 그대로 불러서 화면 없이도 모든 기능을 시험하고 운영하게 한다.
- 예외 하나: 클로즈 베타까지 지울 수 있는 임시 콘솔을 이 저장소의 따로 된 폴더에 둔다(제품의 단위가 아니고 공개된 `/v1`만 부른다. ADR-0016, TD-0007). 제품의 화면과 UX·UI 설계는 프런트엔드 저장소(아직 없다)의 일이다. 이 저장소의 알파 기준은 "모든 기능이 API와 명령줄 클라이언트로 된다"이다.

### 클린 아키텍처의 원칙 (Go의 관례로)

- 단위는 패키지 하나다. 다른 단위는 그 패키지의 공개 면으로만 닿는다.
- 무엇을 import해도 되는지와 어떤 바깥 모듈을 쓰는지는 단위마다 구조 계약에 적고, 검사가 강제한다.
- 포트(인터페이스)는 쓰는 쪽 코어 단위에 둔다. 어댑터는 바깥 층에 둔다. 소스에 닿는 어댑터의 포트에는 쓰는 메서드가 없다(ADR-0008).
- 조립은 시작점 한 곳에서만 한다. 어댑터를 구체적으로 만드는 곳도 거기뿐이다.
- 코어는 운영체제와 네트워크를 모른다. 시계와 난수는 주입받고, 직접 읽는 곳은 바탕 부품의 한 곳뿐이다(REQ-001).
- 순환을 두지 않고, 같은 층의 형제 단위끼리 직접 닿지 않는다.
- 새 외부 모듈은 결정 기록이 있어야 들어온다.
- 현실 시스템에는 읽기만 한다. 실제 툴을 부르는 코드는 어느 층에도 두지 않는다(ADR-0001, ADR-0005).

### 역할 넷

실행 파일 하나가 역할을 골라 뜬다. 개발과 알파에서는 한 프로세스에 함께 띄울 수 있다.

| 역할 | 하는 일 | 여러 벌 |
|---|---|---|
| API | 계약의 길과 MCP. 상태를 쥐지 않는다 | 뜬다 |
| 일꾼 | 수집부터 적용까지와 일정. 소스마다의 동시 상한을 지킨다 | 뜬다 |
| 질의 엔진 | 투영을 메모리에 올려 영향 범위와 길을 답한다. 투영 파일과 변경의 이어짐만 읽는다 | 뜬다 |
| 수집기 | 일꾼의 수집 역할을 소스가 있는 망에 따로 띄운 것 | 망마다 |

질의 엔진은 Go로 직접 만들고, 투영 파일과 변경의 이어짐이 엔진의 경계다.

### 저장소의 자리

- 진실은 PostgreSQL에 둔다. 나머지는 거기서 끌어낸 것이고 다시 만들 수 있다.
- 안쪽의 변경 기록은 포트 뒤에 어댑터 둘(PostgreSQL, 메시지 기록 저장소)을 둘 다 정식으로 둔다.
- 오래된 것은 열린 형식의 파일로 객체 저장소에 내려 둘 수 있다. 분석용 저장소는 끌어낸 것이다.
- 흐름 처리 엔진으로 실행기를 대신하지 않는다. 뜻의 구현이 둘이 되고 결정성을 잃기 때문이다.
- 어느 제품을 쓸지는 구현하는 변경의 결정 기록에서 정한다.

### KG는 여럿이고 정의의 본줄기는 하나다 (ADR-0009)
KG는 계약의 자원이다(`/v1/kgs/{kg}/...`). 운영 KG(`main`), 후보 KG, 시험 KG, 시스템 KG가 있고, 정의의 본줄기는 배포에 하나다. KG는 그 본줄기의 어느 정의 묶음을 쓰는지 가리킨다(`applied_set_no`). 영향 범위는 KG 하나 안에서만 돈다. "KG 나누기"는 쓰기 한계를 푸는 수단이 아니고, 서로 기대지 않는 자산 묶음만 다른 KG가 된다. 한 KG에 수천만 개를 담고 설계의 여유는 수억까지다.

## 의존성의 표 (ADR-0006, REQ-239)
라이브러리를 더하는 변경은 이 표를 함께 고친다. 허용 라이선스만 들인다.

| 구분 | 무엇 | 없을 때 |
|---|---|---|
| 배포의 필수 | PostgreSQL | 없으면 뜨지 않는다 |
| 배포의 필수 | OIDC 제공자(사내 SSO) | 로그인이 없다. 시스템 계정만 쓴다 |
| 배포의 필수 | 으뜸 열쇠의 자리(열쇠 관리 서비스, 하드웨어 보안 모듈, 또는 배포가 주는 열쇠 파일) | 제품에 든 비밀 값 저장소를 쓰지 못한다 |
| 선택 | 메시지 기록 저장소(Kafka 규약. 조직이 이미 운영하는 것) | PostgreSQL 어댑터로 간다 |
| 선택 | 바깥 비밀 값 저장소 | 제품에 든 봉투 암호화 저장소로 간다 |
| 선택 | 파일 보관(S3 규약) | 보관 기한 안에서 PostgreSQL에 둔다 |
| 선택 | 열린 형식으로 관측을 받는 곳 | 제품 안의 신호와 `diagnose`만 쓴다 |
| 들여오는 라이브러리 | 아직 없다. 질의 엔진의 핵심과 해석기는 표준 라이브러리로 짠다. 들인 Go 모듈은 아래 표에 있다 | |
| 들여오는 라이브러리 | YAML 해석(구현하는 변경이 정한다. 쓰는 부분 집합을 직접 읽거나 허용 라이선스의 라이브러리 하나) | 부분 집합을 직접 읽는다 |

기성 그래프 엔진은 들이지 않는다(허용 라이선스, 메모리 상주, 증분 갱신, 과거 버전, 우리의 뜻을 함께 채우는 것이 없다. ADR-0006).

### 들인 Go 모듈 (REQ-239, ADR-0019)

`backend/go.mod`의 `require`는 간접 의존까지 모두 아래 표에 한 줄씩 있어야 한다. 라이선스는 허용 목록(`harness/config/licenses.yaml`)에 있어야 하고, 직접 짜지 않는 까닭이 적혀 있어야 한다. 이 표와 `go.mod`가 다르면 `GODEP010`, `GODEP011`, `GODEP012`가 실패한다. `replace`와 `exclude`는 쓰지 않는다(`GODEP020`).

| 모듈 | 라이선스 | 까닭 | 쓰는 단위 |
|---|---|---|---|
| (아직 없다) | | 백엔드는 표준 라이브러리만 쓴다 | |
