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

> **초기 개발 단계이고, 운영 환경에 쓸 수 없다.** 알파 1을 만드는 중이다. 인증은 개발용 토큰뿐이고, API와 데이터 형식은 예고 없이 바뀔 수 있다.
> 문서는 지금 한국어로만 있다. 영어 문서는 오픈 베타 이후에 제공한다.

## 왜 필요한가

IT 인프라 정보는 여러 시스템에 나뉘어 있다. 자산 대장, 가상화 관리 도구, 네트워크 장비, 설비 시스템이 각자 한 부분을 갖고, 서로 어긋나는 일이 흔하다. 사람은 경험으로 어긋남을 메운다. AI 에이전트는 그러지 못하고, 틀린 그림 위에서는 틀린 조치를 한다. 에이전트에게 운영 업무를 위임하려면 믿을 수 있는 인프라의 그림이 먼저 있어야 한다.

프락시움은 그 그림을 지식 그래프(KG)로 만든다. 서버, 스위치, VM 같은 대상은 객체로, 대상 사이의 연결은 링크로 둔다. 값마다 근거 등급을 붙여서 소스에서 읽은 것(사실), 추정한 것(가정), 모르는 것(알 수 없음)을 구분해 밝힌다. 실제 시스템에는 읽기만 한다. 변경하는 경로가 프락시움 안에 없다.

셀프 호스팅이 기본이다. 표준 모델의 첫 범위는 직접 운영하는 데이터센터 인프라(설비부터 서비스까지)이고, 퍼블릭 클라우드 리소스 계층은 범위 밖이다.

## 지금 되는 것, 아직 안 되는 것

지금 main에서 동작한다.

- 객체, 링크, 액션 타입의 등록부와 표준 모델 데이터(9개 층: 설비, 하드웨어, 네트워크, 스토리지, 가상화, 컨테이너, 플랫폼, 서비스, 공통).
- 합성 데이터센터 생성기. 같은 시드이면 같은 세계가 나온다.
- KG의 중심과 스토리지. 메모리 구현과 PostgreSQL 구현이 있다.
- 파일 소스(CSV, JSON Lines) 하나를 수집해 KG에 넣는 파이프라인(`praxium ingest run`).
- 사전 정의 질의(closed queries) 3종(`find`, `get`, `neighbors`)을 REST API와 명령줄(CLI)로 조회.
- OpenAPI 계약과 그 계약을 지키는지 확인하는 검사.

아직 안 된다.

- **E2E(엔드투엔드) 프로세스의 정의·위임·측정.** 방향만 있고 설계와 구현이 없다. 에이전트가 일하는 기능은 아직 없다.
- 영향 범위 계산 같은 나머지 사전 정의 질의.
- HTTP JSON과 SQL 소스, 스케줄 수집, 수집을 부르는 REST 경로.
- 정의의 브랜치, 제안, 리버트, 실제 인증과 권한(SSO, 역할). 운영 모드로는 서버가 뜨지 않는다.
- 프락시움 자신의 관측성, 시크릿 암호화.
- 웹 UI. 준비 중이다.

## 빠른 시작

Go 1.27.4 이상이 필요하다. PostgreSQL은 없어도 된다.

**1. 빌드한다.**

```
git clone https://github.com/ysk6506/praxium.git
cd praxium/backend
go build -o /tmp/praxium ./cmd/praxium
```

**2. 합성 데이터센터를 만든다.** 실제 데이터가 아니라 시드로 생성한 세계다.

```
/tmp/praxium worldgen -scale 500 -seed 7 -out /tmp/world
```

```
worldgen seed=7 sites=1 racks=1 objects=522 links=932 content_hash=d176229b... elapsed_ms=297
```

**3. 개발 모드로 서버를 실행한다.** 토큰은 임의로 정한 값이다. 개발 모드는 합성 세계를 KG에 바로 적재한다.

```
printf 'dev-token-not-a-secret' > /tmp/dev.tok
/tmp/praxium api serve -mode development -dev-token-file /tmp/dev.tok -dev-truth /tmp/world/truth.jsonl &
```

**4. 조회한다.** 아래 명령은 서버 주소와 토큰을 앞에 둔다. 서버를 끝내려면 `kill %1`을 실행한다.

```
P="/tmp/praxium --endpoint http://127.0.0.1:8080 --token dev-token-not-a-secret"
$P query find --type PhysicalServer --limit 3
```

```
OBJECT_ID                             OBJECT_TYPE     DISPLAY_NAME
obj_1f6bdfd7bf636df9364f3bfaba59e7f0  PhysicalServer  srv-rack.0.0.0.0-4
obj_22222e108e178f05f1da7dc169086347  PhysicalServer  srv-rack.0.0.0.0-8
obj_3a971b8fd37a4faf9d12d1606efdb6d0  PhysicalServer  srv-rack.0.0.0.0-1
```

객체 하나를 읽으면 속성마다 근거 등급(`GRADE`)이 붙어 있다. 합성 세계는 모두 `fact`다.

```
$P query get obj_1f6bdfd7bf636df9364f3bfaba59e7f0
```

```
NAME           VALUE               UNIT  GRADE  BASIS     SEEN_AT               STALE
name           srv-rack.0.0.0.0-4  -     fact   observed  2026-11-14T04:37:14Z  false
manufacturer   Kelmar              -     fact   observed  2026-11-14T04:37:14Z  false
rated_power_w  3141.8              W     fact   observed  2026-11-14T04:37:14Z  false
...
```

이웃한 객체를 링크 타입별로 본다.

```
$P query neighbors --steps 1 obj_1f6bdfd7bf636df9364f3bfaba59e7f0
```

```
LINK_TYPE     MEANING       DIRECTION  PEER_TYPE               COUNT
COOLED_BY     supplied_by   out        CoolingRedundancyGroup  1
MANAGED_BY    managed_by    out        Bmc                     1
MOUNTED_IN    contained_in  out        Rack                    1
...
```

**5. 파일 소스를 수집한다.** 소스 정의와 매핑은 JSON 데이터이고, 샘플 파일은 합성한 값이다. `-times 2`는 같은 파일로 한 번 더 실행해서 두 번째 실행이 아무것도 바꾸지 않음을 보인다. 이 명령은 서버와 별개로, 실행하는 동안만 있는 저장소에 KG를 만든다.

```
D=internal/ingestcheck/testdata
/tmp/praxium ingest run -source $D/source.json -mapping $D/mapping.json -times 2 \
  -input ds_racks=$D/sample/racks.csv -input ds_servers=$D/sample/servers.csv \
  -input ds_hosts=$D/sample/hosts.jsonl -input ds_vms=$D/sample/vms.jsonl -input ds_vm_power=$D/sample/vm-power.csv
```

```
ingest source=src_example_files run=1 objects=10 links=8 ... changed=true new_version=true version=kv_dd2bbdf9...
ingest source=src_example_files run=2 objects=10 links=8 ... changed=false new_version=false version=kv_dd2bbdf9...
```

**PostgreSQL에 저장하려면** `api serve`에 `-store postgres -store-dsn-file <파일>`을 준다. 파일에는 연결 문자열(예: `host=... user=... dbname=...`) 한 줄을 넣는다. 접속 정보가 프로세스 목록과 로그에 남지 않도록 인자나 환경 변수로는 받지 않는다. PostgreSQL 16에서 확인했고, 운영 수준의 설정은 [스토리지의 운영 수준](docs/design-docs/postgres-operations.md)에 있다.

## 핵심 개념

용어는 [용어집](docs/design-docs/glossary.md)을 따른다.

- **지식 그래프(KG).** 실제 IT 인프라를 객체와 링크로 만든 그래프. 값이 바뀌면 변경 배치를 적용해 새 상태를 만들고, 이름을 붙여 굳힌 시점을 확정 버전이라 한다.
- **근거 등급.** 값과 링크마다 `fact`(사실), `assumed`(가정), `unknown`(알 수 없음) 중 하나가 붙는다.
- **표준 모델.** 제품에 들어 있는 객체 타입, 링크 타입, 액션 타입의 목록. 이름은 영어이고, 뼈대는 공개 표준에서 빌렸다.
- **소스와 매핑.** 소스는 데이터를 읽어 오는 현실 시스템이나 파일이다. 매핑은 소스 데이터셋의 컬럼을 객체 타입의 속성에 잇는 정의다.

## 구조

백엔드는 Go이고 `backend/`에 있다. 코드는 단위로 나뉘고, 단위가 무엇을 import할 수 있는지는 설정 파일이 정하며 구조 검사가 강제한다. 서드파티 의존성은 최소로 둔다. 지금 백엔드의 서드파티 모듈은 PostgreSQL 드라이버 하나다. 화면은 다른 리포지토리에서 만들고, REST API 계약으로만 통신한다. 단위와 의존 방향은 [ARCHITECTURE.md](ARCHITECTURE.md)에 있다.

## 로드맵

날짜는 정하지 않고 순서만 둔다. 지금은 알파 1이다.

1. **알파 1.** 파일 소스 하나에서 KG를 만들고 `find`, `get`, `neighbors`로 읽는다. 이 단계부터 직접 써 볼 수 있다.
2. **알파 2.** 수집 확장, 정의의 브랜치와 권한, 영향 범위, 관측성. 객체와 링크 100만 개 수준에서 모든 기능이 API와 CLI로 동작한다.
3. **클로즈드 베타.** 수천만 개 규모를 측정하고 실제 소스를 연결한다.
4. **오픈 베타.** 운영 환경에 나갈 수 있는 수준: 시크릿과 암호화, 자체 운영, 스토리지의 운영 수준.
5. **정식.**

E2E 프로세스(정의·위임·측정)의 구체화는 정식 이후다. 자세한 순서는 [계획](docs/exec-plans/active/plan.md)과 [제품 설계 개요](docs/design-docs/product-design.md)에 있다.

## 이 리포지토리의 작업 방식

코드는 코딩 에이전트가 작성한다. 에이전트가 방향을 잃지 않도록 규칙을 말이 아니라 검사로 강제한다. 의존 방향, 문서의 최신 여부, 테스트를 고쳐서 통과시키지 못하게 하는 잠금, 부채의 기한이 그 대상이다. 변경은 풀 리퀘스트로만 반영하고, 검사 `gate`가 통과해야 병합한다. 에이전트용 안내는 [AGENTS.md](AGENTS.md), 검사의 사용법은 [품질 하네스](docs/design-docs/harness-guide.md), 결정의 이유는 [결정 기록](docs/design-docs/adr)에 있다.

로컬에서는 Node.js 22.12 이상, pnpm, PostgreSQL 16이 필요하다. 일부 Go 테스트가 PostgreSQL을 쓴다.

```
pnpm install --frozen-lockfile
scripts/with-postgres.sh pnpm check
```

`scripts/with-postgres.sh`는 일회용 PostgreSQL을 띄워 명령을 실행하고 끝나면 지운다.

## 기여, 지원, 보안

- **기여.** 환영한다. 기여 절차는 준비 중이고, 개발자 원천 증명(DCO) 방식을 쓸 예정이다. `CONTRIBUTING.md`와 행동 강령도 준비 중이다. 그 전에는 이슈로 먼저 의견을 나눠 달라.
- **지원.** 최선 노력으로 한다. 응답 시간은 약속하지 않는다.
- **보안.** 취약점은 공개 이슈로 올리지 않는다. 비공개 신고 채널은 준비 중이다. 채널이 열릴 때까지는 취약점의 세부 내용을 공개된 곳에 적지 않는다.

## 라이선스와 상표

Apache License 2.0([LICENSE](LICENSE))이다. 함께 나가는 제3자 구성요소는 [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md)에 있다. 라이선스는 코드의 사용을 허락하지만, 프락시움이라는 이름과 로고를 쓸 권한을 주지는 않는다.

이름은 실천과 행동을 뜻하는 그리스어에서 온 praxis에서 땄다. 프락시움은 독립된 제품이고 다른 제품에 의존하지 않는다.

문서 전체는 [색인](docs/index.md)에서 찾는다.
