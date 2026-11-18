---
title: ADR-0080 저장과 질의를 PostgreSQL 하나 위에서 다시 설계한다 — 타입마다 테이블, 질의 스키마, 질의 실행기, 저장한 질의
kind: decision
status: active
enforced_by:
  - none
no_enforcement_reason: 예정인 설계의 결정이다. 구현은 단계마다 자기 결정 기록을 가진 변경이 하고, 그때 요구사항(REQ-551, REQ-552, REQ-560부터 REQ-601까지, REQ-618, REQ-619)마다 테스트와 강제 장치를 붙인다. 지금은 이 결정이 가리키는 문서의 연결(DOC 검사)과 예정 요구사항의 형식(TRACE 검사)만 기계가 본다.
touches_protected:
  - docs/design-docs/adr/0009-kg-storage-and-versions.md
  - docs/design-docs/adr/0010-kg-queries-and-impact.md
  - docs/design-docs/adr/0025-kg-stands-on-a-sequence-of-changes-storage-port-and-reference-implementation.md
  - docs/design-docs/adr/0026-closed-queries-first-three-stand-as-the-reference.md
  - docs/design-docs/adr/0032-postgresql-adapter-closes-the-storage-port-and-td-0011.md
  - docs/design-docs/adr/0034-built-in-file-store-for-the-first-run.md
  - docs/design-docs/adr/0041-cli-usability-local-mode-and-keeping-ingestion-results.md
  - docs/design-docs/adr/0058-sources-from-db-and-api-worker-ingestion-and-editing-screens.md
  - docs/design-docs/adr/0062-kg-read-only-mcp-surface.md
  - docs/product-specs/requirements.md
  - docs/product-specs/requirements-access.md
  - docs/product-specs/requirements-definitions.md
  - docs/product-specs/requirements-ingestion.md
  - docs/product-specs/requirements-ingestion-usability.md
  - docs/product-specs/requirements-model-storage.md
  - docs/product-specs/requirements-operations.md
  - docs/product-specs/requirements-query-api.md
  - docs/product-specs/requirements-web-cli-release.md
updates_docs:
  - ARCHITECTURE.md
  - docs/design-docs/storage-shape.md
  - docs/design-docs/query-schema.md
  - docs/design-docs/query-execution.md
  - docs/design-docs/saved-queries.md
  - docs/design-docs/kg-storage.md
  - docs/design-docs/kg-storage-apply.md
  - docs/design-docs/kg-queries.md
  - docs/design-docs/query-engine-shape.md
  - docs/design-docs/kg-mcp.md
  - docs/design-docs/rest-api.md
  - docs/design-docs/access-control.md
  - docs/design-docs/definitions.md
  - docs/design-docs/ingestion-service.md
  - docs/design-docs/agent-integration.md
  - docs/design-docs/temporary-console.md
  - docs/design-docs/postgres-operations.md
  - docs/design-docs/defaults.md
  - docs/design-docs/product-design.md
  - docs/design-docs/glossary.md
  - docs/design-docs/glossary-general.md
  - docs/product-specs/requirements.md
  - docs/product-specs/requirements-access.md
  - docs/product-specs/requirements-definitions.md
  - docs/product-specs/requirements-ingestion.md
  - docs/product-specs/requirements-ingestion-usability.md
  - docs/product-specs/requirements-model-storage.md
  - docs/product-specs/requirements-operations.md
  - docs/product-specs/requirements-query-api.md
  - docs/product-specs/requirements-web-cli-release.md
amends:
  - adr: "0009"
    what: 결정 4의 적용 3단계 가운데 큰 적용의 읽기 방식(읽기 워터마크 조건의 뷰 두 벌, 작은 적용은 한 트랜잭션, 전체 다시 만들기는 그림자 스키마)과 결정 7의 "타입마다 테이블을 만들지 않는다"(타입마다 테이블, 진실은 속성 번호가 키인 `doc jsonb`, 변동 속성은 35개 타입의 별도 테이블)
  - adr: "0010"
    what: 결정 1의 고정 질의 목록과 결정 2의 응답 엔벨로프와 `serving_pin`과 엔진의 버전 적재, 그리고 투영과 메모리 엔진으로 질의를 계산하는 구조 전체(질의 스키마, 그래프 함수, 질의 실행기, 내장 읽기가 대신한다)
  - adr: "0025"
    what: 결정 11의 메모리 어댑터가 기준이고 테스트와 개발의 스토리지라는 것(PostgreSQL이 기준이 된다)과 결정 10의 읽기 포트의 `Snapshot`(질의 스키마가 대신한다). 포기는 롤백(결정 5)과 읽기 워터마크, 행 번호와 해시 id의 분리(결정 3)는 남는다
  - adr: "0026"
    what: 2부 질의 단위 전체(스냅샷을 훑는 `find`, `get`, `neighbors`, 응답의 숨김 건수, 스냅샷에 고정된 커서)를 내장 읽기가 대신한다. 전체를 뒤집는 것이므로 구현하는 변경이 이 결정 기록을 superseded로 바꾼다
  - adr: "0032"
    what: 결정 8의 저장하는 모양(행의 속성과 출처를 JSONB 하나로 두는 것. 타입마다 테이블로 바뀐다)과 결정 6의 라우팅 계층이 가리키는 저장 모양
  - adr: "0034"
    what: 내장 파일 스토어 전체(PostgreSQL 없이 KG를 저장하는 것)를 걷어낸다. 전체를 뒤집는 것이므로 구현하는 변경이 이 결정 기록을 superseded로 바꾼다
  - adr: "0041"
    what: 결정 3의 수집 결과와 수집 상태를 데이터 폴더의 파일에 남기는 것(수집 상태는 PostgreSQL의 테이블에 둔다)
  - adr: "0058"
    what: 결정 1의 수집 상태 파일 버전 2와 적용 전에 새 상태를 "예정"으로 먼저 쓰는 규칙(수집 상태는 PostgreSQL의 테이블이 된다. 상태를 적용과 같은 트랜잭션에 쓰는 방법은 구현할 때 정한다)
  - adr: "0062"
    what: 결정 2의 툴 목록(`kg_catalog`가 `kg_schema`로 바뀌고, `kg_reachable`이 더해지며, 저장한 질의마다 툴이 하나 더해진다)과 결정 12의 툴 정의 계약 파일이 저장한 질의의 툴을 담는 방식
amends_tests: []
---
# ADR-0080 저장과 질의를 PostgreSQL 하나 위에서 다시 설계한다

## 맥락

제품 책임자가 정했다. 제품은 영향 범위나 이중화 같은 도메인의 뜻을 계산하지 않고 사용자가 질의를 써서 직접 한다(ADR-0077, ADR-0078). 질의는 쉬워야 한다. 사람들이 질의로 자기 조직에 맞는 MCP 툴을 만든다. 여러 단계를 따라가는 질의와 폭발적으로 느는 범위를 감당해야 한다. 규모의 목표는 낮추지 않는다. 한 KG에 객체와 링크 수천만, 설계 여유 수억이고 합성 3천만 객체로 측정해 확인한다(REQ-134). 데모 모드와 영향 범위 계산은 없앤다. 에이전트용 임의 SQL은 필요 없다. 민감 등급은 값만 가린다. 로컬에서도 DB 하나를 함께 띄워도 된다. 외부 그래프 DB 연동은 오픈 베타 이후다.

지금 저장은 속성을 JSONB 하나에 둔 행과 스냅샷을 훑는 질의 엔진이고, 질의는 고정된 사전 정의 질의 목록이다. 이 구조는 위 요구와 맞지 않는다. 사용자가 쓰는 SQL이 읽을 모양(타입 이름의 뷰)이 없고, 큰 단계 수의 질의를 받쳐 줄 함수가 없다. 그래서 원형을 만들어 같은 데이터로 재서 정했다.

측정의 조건(아래 숫자 모두): PostgreSQL 19.0, CPU 2개, 메모리 8GB, `shared_buffers` 1GB, `work_mem` 4MB, JIT 끔, `fsync`와 `synchronous_commit`과 `full_page_writes` 끔, `wal_level=minimal`. 합성 세계(`worldgen -seed 7 -scale 3300000`)의 객체 1,166,114개와 링크 2,080,287개이고 가장 큰 타입(VM)은 127,514행이다. 가장 큰 규모는 객체 약 195만(구성 A만)이다. **3천만 객체는 측정이 아니라 추정이고 한 자릿수의 오차를 각오해야 한다.** PostgreSQL 18, 실제 복제본, 캐시가 빈 상태, `fsync`를 켠 쓰기는 시험하지 않았다.

## 결정

1. **저장소는 PostgreSQL 하나다.** 기준 버전은 18이고 19에만 있는 기능에 기대지 않는다. 메모리 저장소와 파일 저장소를 걷어낸다. 정의, 수집 상태, 동기화 실행 기록, 식별 기록도 PostgreSQL에 둔다. Apache AGE는 기본이 아니다. AGE와 외부 그래프 DB는 뒤의 선택 사항이고, 저장한 질의의 `engine` 칸이 그 자리를 남긴다.
2. **저장 모양은 타입마다 테이블이다.** 진실은 속성 번호가 키인 `doc jsonb`이고(값, 근거 등급, 근거 종류, 관측 시각, 출처), 링크는 테이블 하나(`type_no smallint`, 진짜 컬럼 `grade`와 `basis`)이며 키는 정수다. 타입 있는 컬럼은 뷰가 `doc`에서 꺼낸다. 통계는 표준 속성 전부에 걸고, 인덱스는 "검색에 쓴다"로 표시한 속성과 `oper_state`, `lifecycle_state`에만 둔다. 변동 속성이 있는 35개 타입은 별도 테이블을 두고, 값이 같은 갱신은 쓰지 않는다. 객체 번호(`obj_no`)는 DB 전체에서 이어지고 다시 쓰지 않으며 내용 해시에 들어가지 않는다. 시각은 UTC에 끝이 `Z`인 한 형식이다. 상세는 [새 저장의 모양](../storage-shape.md)이다.
3. **KG마다 저장 스키마 `s_<kg_no>`가 따로 있고, 사용자가 보는 것은 질의 스키마다.** 질의 스키마는 KG와 민감 등급마다 `q_<kg_no>_<grade>`로 있고, 타입 이름의 뷰, 링크 뷰, `object`와 `object_detail`과 `attribute` 뷰, 타입마다 `_at(시점)` 함수, 모델 설명(`COMMENT`와 `model_*` 뷰)으로 이뤄진다. 높은 등급의 스키마에는 낮은 등급과 다른 뷰만 두고 `search_path`로 겹친다. 가리는 것은 값뿐이고 이름은 카탈로그로 보일 수 있다. 스키마 관리기가 모델에서 DDL(테이블, 뷰, 주석, 인덱스)을 만든다. 상세는 [질의 스키마](../query-schema.md)다.
4. **그래프 함수는 `reachable`과 `shortest_path` 둘이다.** 간선은 링크 타입 이름의 목록이나 간선 정의의 뷰 이름으로 주고 질의 글을 받지 않는다. 방문 수가 예산(기본 깊이 12, 방문 20만, 넘을 수 없는 한도 100만)을 넘으면 오류다. 순수 SQL이고 호출자의 권한으로 돌며 복제본에서 돈다.
5. **모든 사용자 질의는 질의 실행기를 지난다.** 조회문으로 감싸기, (KG, 등급)마다의 역할, 읽기 전용 트랜잭션, 위험한 함수의 권한 회수, 시한과 임시 파일 한도, 풀의 나눔(내장 읽기와 저장한 질의 8, 사람의 임의 질의 4), 결과 상한(MCP 200행, REST 1만 행), 감사, 끄기다. 방어마다 적대적 테스트가 있다. 상세는 [질의 실행기와 읽기의 세 경로](../query-execution.md)다.
6. **읽기 경로는 셋이다.** 내장 읽기(찾기, 객체 읽기, 이웃, 도달 집합, 스키마 설명. 서버 코드가 SQL을 만든다), 저장한 질의(파라미터 SQL이 REST와 MCP 툴이 된다), 사람의 임의 질의(질의를 만들고 시험하는 경로. 그 능력이 있는 역할만)다. 에이전트(MCP)에는 임의 SQL이 없다. 저장한 질의와 간선 정의는 정의의 종류이고 같은 id 문법과 변경 절차를 따른다. 상세는 [저장한 질의와 간선 정의](../saved-queries.md)다. 이로써 REQ-250~256의 약속 가운데 응답마다의 "가려진 것의 수"와 "스냅샷에 고정된 커서"는 주지 않는다. 내장 읽기는 근거 등급과 출처를 행에서 그대로 준다.
7. **적용은 세 경로다.** 바뀌는 행이 20만 이하면 한 트랜잭션, 넘으면 2천에서 1만 행의 청크다. 큰 적용 중에도 읽기는 읽기 워터마크까지의 모습을 본다. 질의 스키마를 두 벌 두고 실행기가 KG의 머리 행을 보고 고른다. 전체 다시 만들기는 그림자 저장 스키마에 만들고 한 트랜잭션에 바꿔 단다. 트랜잭션 시한은 60초이고 유휴 트랜잭션 시한을 모든 역할에 건다.
8. **걷어낸다.** 메모리와 파일 저장소, 스냅샷을 훑는 질의 엔진, 사전 정의 질의 13개의 설계, 모델의 "계산을 위한 뜻"(전파, 채널, 인자), 데모 모드(별도 결정)다. 순서는 아래 결과에 있다.

HA와 연결은 [ADR-0081](0081-postgresql-ha-tools-failover-product-rules-and-easy-setup.md)이 정한다.

## 이유

- **구성 C(문서가 진실 + 뷰의 식 + 식 인덱스와 통계)를 고른 이유**(117만 객체, 한 타입 12.7만 행): 인덱스 있는 검색이 0.1~0.3ms, 뷰와 테이블 직접의 비가 0.9~1.3배, 7단계 패턴이 0.5~0.7초다. 모델이 바뀌면 뷰를 다시 만들고(49ms) 인덱스는 온라인으로 만든다. 지금의 적용, 해시, 확정, 복원, 미리 보기의 코드를 그대로 쓴다.
- 질의 스키마를 타입 이름의 뷰로 하면 사용자가 `SELECT name FROM physical_server WHERE …`처럼 쓰고 LLM도 쉽게 쓴다. 테이블 이름은 변하지 않는 번호로 짓고 이름은 뷰에만 써서 타입과 속성의 이름을 바꿔도 행은 그대로다.
- 그래프 함수의 구현 조건은 원형에서 나왔다. 간선 전체를 읽어 두면 예산 1만에도 255MB였다. 단계마다 인덱스로 넓히면 작은 도달이 1.4ms이고 사이트 아래 전체가 1.3초이며 예산 오류는 0.44~5초에 끝난다. 최단 경로를 한쪽에서만 넓히면 3.8~6.6초였다.
- 질의 실행기의 방어는 원형의 적대적 테스트 74건에서 데이터 접근과 설정 변경의 시도가 모두 막혔다. 터지는 재귀 SQL 8개를 30초 동안 함께 돌렸을 때 메모리 1.86GB로 서버가 살아 있었다.
- 사람이 쓰는 SQL을 같은 DB에서 돌리므로 쓰기가 필요한 일(수집, 적용)과 읽기가 같은 DB를 쓰고, 정의와 수집 상태가 같은 DB에 있으면 적용과 상태를 한 트랜잭션에 쓸 수 있다.

## 대안과 기각 사유

- **Apache AGE를 기본으로.** 같은 PostgreSQL 19.0에서 AGE 1.8.0으로 쟀다(20만, 100만 규모). 방향 있는 한 홉 이웃이 SQL 0.2ms에 AGE 3~11ms이고, 방향 없는 한 홉은 20만에서 60초 시한 초과였으며, 링크 타입 없는 도달은 30~50배, 경로 나열은 15~25배 느렸다. 집계와 조인은 같거나 빨랐다. 프로젝트의 상태는 "낮은 활동"이고 열린 오래된 버그가 있으며 관리형 PostgreSQL에서 쓸 수 있는지가 서비스마다 다르다. 실측에서 필요한 구간이 모자랐으므로 기본이 아니다.
- **외부 그래프 DB(FalkorDB, Memgraph, Neo4j)를 주된 길로.** 정본이 둘이 되어 같은 사실이 두 곳에서 따로 바뀐다. 권한, 등급, 이력, 확정이 모두 두 번 필요하다. 오픈 베타 이후의 선택 사항으로 둔다.
- **자체 엔진을 계속.** 메모리에 올린 스냅샷 위의 엔진은 규모(수천만)와 사용자가 쓰는 임의의 조인을 감당하지 못한다. 질의 언어를 새로 만들어 가르쳐야 한다.
- **저장 구성 A(값을 진짜 컬럼에, 출처는 따로).** 가장 작고 빠르다(크기 −4~11%, 적재 +17~27%). 그러나 적용, 해시, 확정, 복원, 미리 보기의 코드를 새로 써야 하고, 이름 변경에 이력이 깨지며, "모른다"와 "값 없음"의 구분을 따로 증명해야 한다.
- **저장 구성 B(문서 + 저장 생성 컬럼).** 읽기는 A와 같다. 컬럼을 더하는 모델 변경이 테이블을 잠그고 다시 쓴다(12.7만 행에 1초, 읽기가 최대 0.9초 막혔다. 3천만 객체의 큰 타입은 약 26초로 추정). "테이블을 다시 쓰는 스키마 변경 금지"에 걸린다.
- **범용 뷰**(속성을 이름 키 jsonb로). 속성 검색이 308ms(단순 테이블 1.4ms)이고 7단계 패턴이 2.9초(86ms)이며 크기가 2.5배다. 속성 인덱스를 못 탄다.
- **속성 원장(EAV).** 이력과 출처에는 자연스럽지만 읽기가 피벗이고 100만 객체에 수천만 행이다.
- **한 번의 긴 트랜잭션으로 모든 적용.** 청소를 막아 상태 테이블이 부풀고(25초 열린 읽기에 +60MB) WAL이 한 번에 쏟아진다.
- **큰 적용 중에는 읽기를 막기.** 에이전트가 길게 멈춘다. 읽기 워터마크 뷰로 막지 않는다.
- **에이전트에게 임의 SQL 툴.** 필요 없다고 제품 책임자가 정했고, 주입과 폭주의 길이다. 저장한 질의가 대신한다.
- **행 단위 제한을 장벽 뷰로.** 7단계 패턴이 0.54초에서 53초로 약 100배 느려졌다. 방법은 미결이다.
- **내장 PostgreSQL을 제품에 묶기.** 새 의존이 필요하다. 묶음 실행 파일(컴포즈)과 진단 명령으로 로컬의 첫 경험을 줄인다.
- **데모의 샘플 데이터를 저장에 맞춰 유지.** 데모 모드를 걷어내는 결정이 따로 있다.

## 결과

- **한도와 미결.** 3천만의 실제 측정(더 큰 기계가 필요하다), PostgreSQL 18에서의 확인, `fsync`를 켠 쓰기, 실제 복제본, 읽기 워터마크 뷰 두 벌의 이력 인덱스 비용, 시각 식 인덱스로 하는 범위 검색(글자 그대로 두면 26~500배 느렸다), 행 단위의 범위 제한, 테스트 기반의 교체와 잠긴 테스트의 크기, 드라이버의 장애 조치 동작, 커스텀 속성의 이름 규칙, `object` 뷰의 타입 이름 붙이기의 비용, `object_detail`의 함수 권한을 아직 정하지 않았다. 로컬의 첫 경험은 PostgreSQL을 함께 띄우므로 무거워진다.
- **뒤집는 것**: ADR-0010(사전 정의 질의 13개, 투영과 메모리 엔진), 스냅샷을 훑는 질의 엔진, ADR-0032의 저장 모양, ADR-0034와 메모리 저장소, ADR-0041의 데이터 폴더로 KG를 내주는 부분, ADR-0062의 툴 목록, REQ-250~256의 일부 약속, 문서의 "파티션 + UNLOGGED" 준비 테이블 행, CI의 PostgreSQL 16(18로).
- **고치는 것**: REQ-132와 REQ-249(큰 적용의 청크는 남고 읽기 방식이 뷰 두 벌로), ADR-0025의 해당 부분, 수집 설계의 "예정 상태"와 상태 파일(테이블로).
- **남는 것**: 표준 모델의 소유와 구조, 조직 속성, 변경 번호와 확정과 해시와 세대, 식별과 병합, 수집의 설계(ADR-0058), 정의의 오류와 id 문법, 시크릿 저장소의 코어, MCP 전송, 화면의 쓰기 기반, 변경 미리 보기, 상시 가동 에이전트(ADR-0074), 접근 토큰과 역할, REQ-134.
- **구현 순서**: 1단계 운영 데이터의 PostgreSQL 저장과 연결 규칙(CI를 18로) / 2단계 스키마 관리기와 새 KG 저장과 적합성 시험 / 3단계 질의 실행기, 질의 스키마, 그래프 함수, 내장 읽기, 적대적 테스트 / 4단계 저장한 질의, 간선 정의, MCP 툴, 질의 화면 / 5단계 메모리와 파일 저장소, 스냅샷 엔진, 모델의 계산용 뜻을 걷어내기 / 6단계 HA 본보기와 점검 명령, 규모 검증 도구, 복제본으로 읽기 보내기.
- **규모 검증 도구**를 저장소에 넣어 합성 세계의 생성, 적재, 관문 질의, 보고서를 한 도구로 한다. 3천만의 수치는 이 도구가 재기 전까지 추정이다.
- 이 결정은 설계의 결정이고 코드와 하네스 설정은 바꾸지 않는다. 지금 코드의 메모리와 파일 저장소와 스냅샷 엔진은 걷어내는 단계까지 남는다. 요구사항 REQ-551, REQ-552, REQ-560~601, REQ-618, REQ-619는 예정이다.
