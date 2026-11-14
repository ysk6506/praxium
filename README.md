---
title: Praxium — IT 인프라를 믿을 수 있는 지식 그래프로 만드는 오픈소스 플랫폼
kind: guide
status: active
covers:
  - package.json
follows:
  - docs/exec-plans/active/plan.md
---
# Praxium

**IT 인프라를 믿을 수 있는 지식 그래프(KG)로 만드는 오픈소스 플랫폼. 그 위에서 AI 에이전트에게 운영 업무를 위임하고 결과를 측정하는 것이 목표다.**

[![ci](https://github.com/ysk6506/praxium/actions/workflows/ci.yml/badge.svg)](https://github.com/ysk6506/praxium/actions/workflows/ci.yml)

> **초기 개발 단계이며, 운영 환경에서는 쓸 수 없습니다.** 알파 1을 만드는 중입니다. 인증은 개발용 토큰뿐이고, API와 데이터 형식은 예고 없이 바뀔 수 있습니다.
> 문서는 지금 한국어로만 제공합니다. 영어 문서는 오픈 베타 이후에 제공합니다.

## 무엇을 할 수 있나요

- `praxium demo` 한 줄로 합성한 데이터센터를 띄우고, 브라우저에서 서버, 스위치, VM과 그 연결을 둘러볼 수 있습니다.
- 엑셀, CSV, JSON Lines 파일을 읽어 자기 데이터로 KG를 만들 수 있습니다(`praxium ingest`).
- 만든 KG를 명령줄(`praxium kg`, `types`, `query`)과 REST API로 조회할 수 있습니다.

## 왜 필요한가요

IT 인프라 정보는 여러 시스템에 나뉘어 있습니다. 자산 대장, 가상화 관리 도구, 네트워크 장비, 설비 시스템이 각자 한 부분을 갖고 있고, 서로 어긋나는 일이 흔합니다. 사람은 경험으로 어긋남을 메웁니다. AI 에이전트는 그러지 못하고, 틀린 그림 위에서는 틀린 조치를 합니다. 에이전트에게 운영 업무를 위임하려면 믿을 수 있는 인프라의 그림이 먼저 있어야 합니다.

프락시움은 그 그림을 지식 그래프(KG)로 만듭니다. 서버, 스위치, VM 같은 대상은 객체로, 대상 사이의 연결은 링크로 둡니다. 값마다 근거 등급을 붙여서 소스에서 읽은 것(사실), 추정한 것(가정), 모르는 것(알 수 없음)을 구분해 밝힙니다. 실제 시스템에는 읽기만 합니다. 변경하는 경로가 프락시움 안에 없습니다.

셀프 호스팅이 기본입니다. 표준 모델의 첫 범위는 직접 운영하는 데이터센터 인프라(설비부터 서비스까지)이고, 퍼블릭 클라우드 계층은 다음 단계에서 추가합니다.

## 지금 되는 것, 아직 안 되는 것

지금 main에서 동작합니다.

- 객체, 링크, 액션 타입의 레지스트리와 표준 모델 데이터(9개 층).
- 합성 데이터센터 생성기. 같은 시드이면 같은 세계가 나옵니다.
- 실행 파일에 포함된 웹 UI(임시 콘솔). 타입, 객체 테이블, 객체 상세(근거 등급 포함), 이웃을 읽기 전용으로 보여 줍니다.
- 데모 모드(`praxium demo`)와 로컬 모드(`praxium local`).
- 엑셀, CSV, JSON Lines 소스의 수집(`praxium ingest`).
- 내장 파일 스토어와 PostgreSQL 스토리지.
- 사전 정의 질의(closed queries) 3종(`find`, `get`, `neighbors`)을 REST API와 명령줄(CLI)로 조회.

아직 안 됩니다.

- **E2E(엔드투엔드) 프로세스의 정의·위임·측정.** 방향만 있고 설계와 구현이 없습니다. 에이전트가 일하는 기능은 아직 없습니다.
- 운영 모드와 실제 인증·권한(SSO, 역할). 지금 서버는 개발용 토큰으로만 뜹니다.
- 데이터베이스와 REST API 소스, 스케줄 수집.
- 영향 범위 계산 같은 나머지 사전 정의 질의.
- 매핑을 편집하는 화면. 매핑 정의는 지금 JSON 파일로 직접 씁니다.
- 정의의 브랜치와 제안, 프락시움 자신의 관측성, 시크릿 암호화.

## 빠른 시작

### 1. 내려받기

[최신 릴리스](https://github.com/ysk6506/praxium/releases/latest)에서 자기 운영체제에 맞는 파일(`praxium_<버전>_<os>_<arch>`)을 받습니다. 리눅스와 맥은 `.tar.gz`, 윈도우는 `.zip`입니다. 첫 릴리스(`v0.1.0-alpha.1`)는 곧 나옵니다. 받은 파일은 릴리스의 `SHA256SUMS`로 확인하고 압축을 풉니다.

```
sha256sum -c SHA256SUMS --ignore-missing
tar xzf praxium_<버전>_linux_amd64.tar.gz
cd praxium_<버전>_linux_amd64
./praxium version
```

```
praxium 0.1.0-alpha.1
...
web ui:  included
```

맥과 윈도우용 파일에는 서명이 없습니다. 처음 실행할 때 운영체제가 확인되지 않은 개발자의 프로그램이라는 경고를 띄웁니다. 직접 받은 파일이 맞는지 `SHA256SUMS`로 확인한 뒤에 운영체제의 보안 설정에서 실행을 허용해 주세요.

### 2. 1분 데모

```
./praxium demo
```

```
Praxium demo: the data is synthetic and not connected to any real system. This is not for production.
Loaded 7236 objects and 12874 links.
Open this address in a browser (the token is after the #, which a browser does not send to the server):
http://127.0.0.1:8080/#/connect?token=...
```

출력된 주소를 브라우저로 엽니다. 토큰은 주소에 들어 있어서 입력할 것이 없습니다. 타입 목록에서 시작해 객체 테이블, 객체 상세, 이웃 객체로 이동하며 둘러볼 수 있습니다. 데이터는 합성한 것이고 메모리에만 있습니다. 끝내려면 Ctrl+C를 누릅니다.

### 3. 내 파일로 해 보기

예제 파일을 만들고, 파일을 살펴보고, 읽어 들이고, 결과를 브라우저로 봅니다. 엑셀과 CSV, JSON Lines 파일을 읽습니다.

```
./praxium ingest init -out ex
./praxium ingest inspect -input ex/excel/datacenter.xlsx
./praxium ingest run -data-dir data -source ex/csv/source.json -mapping ex/csv/mapping.json \
  -input ds_racks=ex/csv/racks.csv -input ds_servers=ex/csv/servers.csv -input ds_hosts=ex/csv/hosts.jsonl \
  -input ds_vms=ex/csv/vms.jsonl -input ds_vm_power=ex/csv/vm-power.csv
./praxium local -data-dir data
```

- `ingest init`은 예제 소스 정의, 매핑 정의, 샘플 파일(합성한 값)을 씁니다. 다음에 실행할 명령도 알려 줍니다.
- `ingest inspect`는 매핑을 쓰기 전에 파일의 시트, 컬럼, 값의 모양을 보여 줍니다.
- `ingest run`은 파일을 읽어 `data` 폴더에 KG로 남깁니다. 파일은 읽기만 합니다.
- `local`은 그 KG를 웹 UI와 API로 보여 주고, 접속 주소를 출력합니다. 서버가 `data` 폴더를 열고 있는 동안에는 `ingest run`을 하지 않습니다.

```
ingest source=src_example_files run=1 objects=10 links=8 ... changed=true new_version=true version=kv_df230d8a...
completed without problems
The result is kept in data (KG main). Look at it with: praxium local -data-dir data
```

자기 데이터에는 소스 정의와 매핑 정의 JSON을 직접 써야 합니다. 예제 파일이 출발점입니다. 형식은 [현실 데이터 수집](docs/design-docs/ingestion.md)에 있습니다.

### 4. 명령 한눈에

`./praxium --help`가 모든 명령을 보여 주고, `./praxium <명령> --help`가 사용법과 예를 보여 줍니다.

| 명령 | 하는 일 |
|---|---|
| `demo` | 합성 데이터센터와 웹 UI를 띄웁니다 |
| `local` | 내 파일로 만든 KG를 웹 UI와 API로 보여 줍니다 |
| `ingest init`, `inspect`, `run` | 예제 만들기, 파일 살펴보기, 파일을 읽어 KG로 만들기 |
| `kg`, `types`, `object`, `query` | 실행 중인 서버에 KG, 타입, 객체를 묻습니다 |
| `version` | 버전을 보여 줍니다 |

조회 명령은 서버 주소와 토큰이 필요합니다. `demo`와 `local`이 토큰을 출력합니다.

```
./praxium --endpoint http://127.0.0.1:8080 --token <토큰> query find --type PhysicalServer --limit 3
```

```
OBJECT_ID                             OBJECT_TYPE     DISPLAY_NAME
obj_2c5803dfabbf12d5fa85aa5b8731fb2b  PhysicalServer  SV-2003
obj_87366b3a5f92cbee9f0012df38d2674d  PhysicalServer  SV-2001
...
```

### 스토리지

- **내장 파일 스토어**가 기본입니다. `-data-dir` 폴더 하나에 KG를 저장하고 다른 프로그램이 필요 없습니다. 권장 범위는 객체 수십만 개 이하, 서버 한 대입니다(한 번의 측정에 근거한 값이고 기준선이 아닙니다, ADR-0034).
- **PostgreSQL**은 그보다 큰 규모에 씁니다. `api serve`에 `-store postgres -store-dsn-file <파일>`을 줍니다. 파일에는 연결 문자열 한 줄을 넣습니다. 운영 수준의 설정은 [스토리지의 운영 수준](docs/design-docs/postgres-operations.md)에 있습니다.

## 핵심 개념

용어는 [용어집](docs/design-docs/glossary.md)을 따릅니다.

- **지식 그래프(KG).** 실제 IT 인프라를 객체와 링크로 만든 그래프입니다. 변경 배치를 적용해 상태를 만들고, 이름을 붙여 굳힌 시점을 확정 버전이라 합니다.
- **근거 등급.** 값과 링크마다 `fact`(사실), `assumed`(가정), `unknown`(알 수 없음) 중 하나가 붙습니다.
- **표준 모델.** 제품에 들어 있는 객체 타입, 링크 타입, 액션 타입의 목록입니다. 이름은 영어이고, 뼈대는 공개 표준에서 빌렸습니다.
- **소스와 매핑.** 소스는 데이터를 읽어 오는 현실 시스템이나 파일입니다. 매핑은 소스 데이터셋의 컬럼을 객체 타입의 속성에 잇는 정의입니다.

## 구조

백엔드는 Go이고 `backend/`에 있습니다. 웹 UI는 같은 리포지토리의 `apps/web`에 있고 실행 파일에 포함됩니다(ADR-0035). 웹 UI는 공개된 REST API만 부르는 임시 콘솔이며, 클로즈드 베타 이후에는 별도의 화면으로 대체할 계획입니다. 코드는 단위로 나뉘고, 단위가 무엇을 import할 수 있는지는 설정 파일이 정하며 구조 검사가 강제합니다. 서드파티 의존성은 최소로 둡니다. 백엔드의 서드파티 모듈은 PostgreSQL 드라이버 하나이고, 웹 UI의 런타임 의존성은 React와 Base UI입니다. 단위와 의존 방향은 [ARCHITECTURE.md](ARCHITECTURE.md)에 있습니다.

## 로드맵

날짜는 정하지 않고 순서만 둡니다. 지금은 알파 1입니다.

1. **알파 1.** 파일 소스 하나에서 KG를 만들고 `find`, `get`, `neighbors`로 읽습니다. 이 단계부터 직접 써 볼 수 있습니다.
2. **알파 2.** 수집 확장, 정의의 브랜치와 권한, 영향 범위, 관측성. 객체와 링크 100만 개 수준에서 모든 기능이 API와 CLI로 동작합니다.
3. **클로즈드 베타.** 수천만 개 규모를 측정하고 실제 소스를 연결합니다.
4. **오픈 베타.** 운영 환경에 나갈 수 있는 수준을 목표로 합니다. 시크릿과 암호화, 자체 운영, 스토리지의 운영 수준을 갖춥니다.
5. **정식.**

E2E 프로세스(정의·위임·측정)의 구체화는 계획 문서에서 정식 이후의 순서입니다. 지금은 방향만 있습니다. 자세한 순서는 [계획](docs/exec-plans/active/plan.md)과 [제품 설계 개요](docs/design-docs/product-design.md)에 있습니다.

## 이 리포지토리의 작업 방식

코드는 코딩 에이전트가 작성합니다. 에이전트가 방향을 잃지 않도록 규칙을 말이 아니라 검사로 강제합니다. 의존 방향, 문서의 최신 여부, 테스트를 고쳐서 통과시키지 못하게 하는 잠금, 부채의 기한이 그 대상입니다. 변경은 풀 리퀘스트로만 반영하고, 검사 `gate`가 통과해야 병합합니다. 에이전트용 안내는 [AGENTS.md](AGENTS.md), 검사의 사용법은 [품질 하네스](docs/design-docs/harness-guide.md), 결정의 이유는 [결정 기록](docs/design-docs/adr)에 있습니다.

소스에서 빌드하려면 Go 1.27.4 이상, Node.js 22.12 이상, pnpm이 필요합니다. 웹 UI까지 담긴 실행 파일은 이렇게 만듭니다.

```
pnpm install --frozen-lockfile
scripts/build-with-web.sh dist/praxium
```

검사는 `scripts/with-postgres.sh pnpm check`로 실행합니다. 일부 Go 테스트가 PostgreSQL 16을 쓰며, 이 스크립트가 일회용 서버를 띄웠다가 끝나면 지웁니다.

## 기여, 지원, 보안

- **기여.** 환영합니다. 기여 절차는 준비 중이며 개발자 원천 증명(DCO) 방식을 쓸 예정입니다. `CONTRIBUTING.md`와 행동 강령도 준비 중입니다. 그 전에는 이슈로 먼저 의견을 나눠 주세요.
- **지원.** 최선 노력으로 합니다. 응답 시간은 약속하지 않습니다.
- **보안.** 취약점은 공개 이슈로 올리지 마세요. 비공개 신고 채널은 준비 중입니다. 채널이 열릴 때까지는 취약점의 세부 내용을 공개된 곳에 적지 말아 주세요.

## 라이선스와 상표

Apache License 2.0([LICENSE](LICENSE))입니다. 함께 나가는 제3자 구성요소는 [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md)에 있습니다. 라이선스는 코드의 사용을 허락하지만, 프락시움이라는 이름과 로고를 쓸 권한을 주지는 않습니다.

이름은 실천과 행동을 뜻하는 그리스어에서 온 praxis에서 땄습니다. 프락시움은 독립된 제품이고 다른 제품에 의존하지 않습니다.

문서 전체는 [색인](docs/index.md)에서 찾을 수 있습니다.
