---
title: 설계 조사 — 결정의 근거가 된 조사의 요약과 한계
kind: reference
status: active
follows:
  - docs/design-docs/adr/0006-go-backend-clean-architecture-backend-first.md
---
# 설계 조사

ADR-0005~0018을 정하기 전에 공개 자료를 읽고 작은 시제품으로 잰 것을 줄인 문서예요. 결정은 각 결정 기록에 있고, 여기에는 **무엇을 확인했고 무엇을 못 했는지**만 적어요. 구현 전에 숫자와 제한 값은 원문으로 다시 봐야 해요.

## 읽는 법

- **확인**: 그 주소를 열어 내용을 봤어요. **미확인**: 열지 못했거나 제목만 봤거나 본문에 그 내용이 없었어요.
- 페이지는 요약을 거쳐 읽었어요. 따옴표 안의 글과 숫자는 한 글자씩 대조하지 못했어요.
- 시제품의 숫자는 작은 개발용 장비 하나에서 잰 값이에요. 제품의 목표 규모를 증명하지 않아요. 규모의 숫자는 계획 10번에서 지어낸 세계로 다시 재요.

## 1. 정의의 버전과 브랜치 (ADR-0014)

물음: 변경을 브랜치로 모아 검사하고 병합하고, 되돌리는 일을 어떤 모양으로 할까.

확인한 것:
- 원본 주소를 주고받는 큐와 trunk 포인터의 비교-교환 갱신으로 병합을 직렬화하는 방식([Anthropic: Redesigning source control](https://www.anthropic.com/research/redesigning-source-control)).
- 변경 이력을 되돌리기와 숨기기로 다루는 방식([Sapling 문서](https://sapling-scm.com/docs/overview/intro)), 작업 기록을 남기는 방식([jj 작업 기록](https://docs.jj-vcs.dev/latest/operation-log/)), 충돌을 값으로 남기는 방식([jj 충돌](https://docs.jj-vcs.dev/latest/conflicts/)).
- 세 방향 병합과 충돌 표([DoltHub 충돌](https://dolthub.com/docs/concepts/dolt/git/conflicts), [세 방향 병합](https://www.dolthub.com/blog/2020-07-15-three-way-merge)).
- 병합 전 검증과 대기열([Zuul gating](https://zuul-ci.org/docs/zuul/latest/gating.html)), 병합과 되돌리기의 규칙([git-merge](https://git-scm.com/docs/git-merge), [git-revert](https://git-scm.com/docs/git-revert)).
- 서버 쪽 적용에서 필드 소유([Kubernetes 서버 사이드 적용](https://raw.githubusercontent.com/kubernetes/website/main/content/en/docs/reference/using-api/server-side-apply.md)), 설정 변경의 안전([Holistic Configuration Management](https://web.eecs.utk.edu/~qcao1/cs560/papers/holistic.pdf)).
- 상태 파일과 되돌리기([Terraform state](https://developer.hashicorp.com/terraform/language/state), [Liquibase rollback](https://docs.liquibase.com/commands/rollback/rollback.html), [Atlas](https://atlasgo.io/versioned/diff)).

못 한 것:
- OpenAI의 Git 확장 글은 원문을 열지 못했고, 2차 요약으로만 봤어요. 그 요약이 원문과 같은지 모르니 근거로 쓰지 않았어요.
- Google의 단일 저장소 논문은 초록만 읽었어요. Sapling의 변이 이력 내부 문서, Mononoke의 본 문서, Dolt의 병합 공식 문서 본문은 읽지 못했어요.
- Palantir Foundry는 온톨로지 브랜치가 있다는 사실만 확인했어요. 내용은 3절에서 따로 다뤄요.

쓴 곳: 정의 묶음이 불변이고 내용 해시를 가지는 것, 브랜치가 기준 묶음 대비 덮개인 것, 병합이 직렬 큐에서 검증하고 후보 실행을 한 뒤 포인터를 옮기는 것, 되돌리기가 새 묶음을 더하는 것.

## 2. 표준 모델의 뼈대 (ADR-0007)

물음: 객체 타입과 링크 타입의 이름과 골격을 어디서 빌릴까. 빌리는 것은 이름, 골격, 식별자, 열거값까지예요. 링크의 뜻, 번짐, 근거, 시간은 프락시움이 정해요.

확인한 것(열어 본 주소):
- 설비와 전원과 냉각: [Redfish Chassis](https://redfish.dmtf.org/schemas/v1/Chassis.v1_26_0.json), [PowerDistribution](https://redfish.dmtf.org/schemas/v1/PowerDistribution.v1_7_0.json), [CoolingUnit](https://redfish.dmtf.org/schemas/v1/CoolingUnit.v1_6_0.json), [Facility](https://redfish.dmtf.org/schemas/v1/Facility.v1_4_2.json).
- 하드웨어 구성품과 묶음 링크: [OpenConfig platform types](https://raw.githubusercontent.com/openconfig/public/master/release/models/platform/openconfig-platform-types.yang), [OpenConfig 집계 인터페이스](https://raw.githubusercontent.com/openconfig/public/master/release/models/interfaces/openconfig-if-aggregate.yang).
- 네트워크 층: [RFC 8345](https://www.rfc-editor.org/rfc/rfc8345), [RFC 8944](https://www.rfc-editor.org/rfc/rfc8944), [RFC 8346](https://www.rfc-editor.org/rfc/rfc8346).
- 카탈로그와 관계 짝: [Backstage 서술자](https://backstage.io/docs/features/software-catalog/descriptor-format/), [NetBox 모델](https://netboxlabs.com/docs/netbox/models/dcim/device/).
- 실행 환경: [OpenTelemetry k8s](https://opentelemetry.io/docs/specs/semconv/resource/k8s/), [host](https://opentelemetry.io/docs/specs/semconv/resource/host/), [Gateway API](https://gateway-api.sigs.k8s.io/concepts/api-overview/), [OpenStack aggregates](https://docs.openstack.org/nova/latest/admin/aggregates.html).
- 저장소와 서비스: [Swordfish](https://www.snia.org/forums/smi/swordfish), [ServiceNow CSDM 용어](https://www.servicenow.com/docs/r/GCPjp~ldPTNRN99oH1DYFg/qX~bOXG7Mm~QFYI7SZVbaQ).

바로잡은 것: 223P는 제안 단계의 자료이고 표준으로 발간된 것이 아니에요. 앞선 서술을 고쳤고, 223P에는 기대지 않아요([open223](https://open223.info/)).

못 한 것: Project Brick의 관계 목록(가져오기가 여러 번 실패)에는 어떤 결정도 기대지 않았어요. NetBox의 다른 모델 페이지, OpenConfig의 network-instance·bgp·lacp·lldp 모델도 읽지 못했어요.

## 3. 받기와 스트리밍 (ADR-0008)

물음: 배치와 흐름에 같은 정의를 돌릴 수 있는가, 소스마다 이어받기와 삭제 알기가 어떻게 다른가.

확인한 것:
- Foundry의 데이터 연결과 스트리밍 문서(주소는 `palantir.com/docs/foundry/` 아래): 데이터 연결의 개요와 구조와 핵심 개념, 동기화 설정, 파일 기반 동기화, 스트리밍 동기화, 밀어넣기 수집, 연결 보안, 문제 해결. 배치와 스트림의 비교표에서 변환의 지원 차이, 상태와 창의 한계, 열쇠로만 보장되는 순서, 일관성과 지연의 맞바꿈이 있어요. 이것이 "정의 하나가 배치와 흐름에 똑같이 돈다"(원칙 8)를 세운 까닭이에요.
- 소스별 전달 보장: [Kubernetes API](https://kubernetes.io/docs/reference/using-api/api-concepts/)의 watch와 410, [gNMI 명세](https://github.com/openconfig/reference/blob/master/rpc/gnmi/gnmi-specification.md)의 구독 모드, vSphere의 속성 수집기.
- 증분 계산: [DBSP](https://www.vldb.org/pvldb/vol16/p1601-budiu.pdf), [Feldera](https://github.com/feldera/feldera), [RisingWave](https://github.com/risingwavelabs/risingwave), [Beam](https://beam.apache.org/documentation/programming-guide/).

못 한 것: OpenStack 알림의 전달 보장, Redfish 이벤트 서비스의 재시도 규칙은 확인하지 못했어요. syslog와 SNMP trap의 보장은 일반 지식이에요. Foundry 스트리밍 변환의 전체 지원표도 못 봤어요.

주의: Foundry의 단위 테스트에 대해 앞선 조사가 "없다"고 적었는데, 지금의 공개 문서에는 제안을 병합하기 전에 단위 테스트를 확인하는 페이지가 있어요. 그래서 "Foundry는 테스트가 약하다"를 프락시움의 차별점으로 쓰지 않아요.

## 4. 언어와 구조 (ADR-0006)

물음: 백엔드 언어와 큐, 데이터베이스 고가용을 무엇으로 할까.

확인한 것:
- 에이전트와 도구의 연결 표준의 SDK 등급([MCP SDK](https://modelcontextprotocol.io/docs/sdk), [등급 기준](https://modelcontextprotocol.io/community/sdk-tiers)).
- PostgreSQL 위의 작업 큐: [pg-boss](https://github.com/timgit/pg-boss), [Graphile Worker](https://github.com/graphile/worker), [River](https://github.com/riverqueue/river), [pgmq](https://github.com/pgmq/pgmq). LISTEN/NOTIFY가 규모에서 한계가 있다는 글([Recall.ai](https://recall.ai/blog/postgres-listen-notify-does-not-scale)).
- PostgreSQL 고가용 오퍼레이터: [CloudNativePG](https://github.com/cloudnative-pg/cloudnative-pg)와 [복제 문서](https://cloudnative-pg.io/docs/1.28/replication/). 동기 복제로 RPO 0을 만드는 설정을 확인했어요. Patroni, Zalando, Crunchy의 라이선스는 미확인이에요.
- 그래프 저장소와 분산 SQL의 라이선스와 상태: [Neo4j](https://neo4j.com/licensing/), [FalkorDB](https://github.com/FalkorDB/FalkorDB), [Memgraph](https://github.com/memgraph/memgraph), [YugabyteDB](https://github.com/yugabyte/yugabyte-db), [TiDB](https://github.com/pingcap/tidb).
- 공급망: [pnpm의 npm 공급망 방어](https://pnpm.io/blog/2025/12/05/newsroom-npm-supply-chain-security).

못 한 것: 언어별 에이전트 코딩 성능의 비교는 벤더 글과 논문 초안에 기대어 근거가 약해요. 결정의 근거로는 약하다고 보고 구조(진실의 자리 하나, 의존 방향)와 사용 가능한 부품 쪽에서 정했어요. 서버 쪽 도구(go-licenses, 열거 검사 린터, OpenAPI 생성기)의 최신 상태도 못 봤어요.

## 5. 변경의 처리와 저장 (ADR-0008, ADR-0009)

물음: 받은 변경을 정제하고 식별해서 저장하는 길의 처리량과 저장 비용.

작은 장비 하나에서 잰 값이에요(2 vCPU, 메모리 약 8GB, 로컬 PostgreSQL 16, 사건 200만 건, 건당 약 275바이트). 세 번 돌린 범위예요.

| 구현 | 한 스레드, 사건/초 | 두 스레드, 사건/초 | 조건과 한계 |
|---|---|---|---|
| Node 22 | 21만~22만 | 39만~40만 | 파일 전체를 읽는 시험 구조라 메모리 값은 비교에 쓰지 않아요 |
| Go 표준 JSON | 19만 | 33만 | 메모리 약 0.1GB |
| Go 손으로 짠 해석기 | 48만 | 75만~81만 | 이스케이프를 못 다뤄 실서비스에는 맞지 않아요 |

| 저장 값(PostgreSQL) | 잰 값 | 조건 |
|---|---|---|
| `COPY` 100만 행 | 약 88만 행/초 | 좁은 행, 색인 없음 |
| 병합(스테이징, 내용 해시가 다를 때만 갱신, 변경 기록 추가) | 묶음 1천에서 약 7.5만, 1만에서 약 10만, 10만에서 약 13만 행/초 | 모두 바뀐 최악, 기본 키만 있음 |
| 같은 병합에 바뀌는 열의 보조 색인 | 약 절반 | HOT 갱신이 안 돼요 |
| WAL | 행당 약 380~430바이트 | 묶음 1천에서는 약 1.7KB |

읽는 법: 내용 해시가 같은 변경(약 절반)은 데이터베이스에 닿기 전에 거르는 설계가 이 숫자에서 나왔어요. 이 숫자는 한 장비의 한 시험이고, 제품의 규모 약속이 아니에요. 규모는 지어낸 세계로 다시 재요.

못 한 것: Go의 그래프 시험과 지도 시험 값은 이 문서에 옮기지 않았어요. 메모리 비교는 시험 구조의 차이가 있어 계획 10번에서 다시 재요. PostgreSQL에서 시퀀스 번호가 커밋 순서와 다를 수 있어서, KG마다 쓰는 쪽을 하나로 두는 결정을 했어요(ADR-0009).

## 6. 에이전트 시대의 버전 관리와 PostgreSQL의 운영 수준 (ADR-0014, ADR-0009)

물음: 에이전트가 정의 변경의 대부분을 쓰면 병합 큐와 검토와 되돌리기는 어떻게 달라지고, 쓰기 중심의 PostgreSQL을 운영 수준까지 어떻게 두나.

확인한 것(주소를 열어 봤어요):
- 에이전트 시대의 푸시와 병합 큐를 다룬 글과 딸린 글: [GeekNews 요약](https://news.hada.io/article/git-push-in-the-agent-era), [Aspen](https://news.hada.io/topic?id=36787), [Zed Delta](https://news.hada.io/topic?id=36761), [코드가 검토를 앞지른다](https://news.hada.io/article/code-outruns-review).
- 대규모 PostgreSQL 운영: [OpenAI의 PostgreSQL 확장 글](https://openai.com/index/scaling-postgresql/)과 [2차 요약](https://www.pixelstech.net/article/1747708863-openai%3a-scaling-postgresql-to-the-next-level).
- 공식 문서(PostgreSQL 18): [청소](https://www.postgresql.org/docs/current/routine-vacuuming.html), [HOT](https://www.postgresql.org/docs/current/storage-hot.html), [논리 복제의 설명](https://www.postgresql.org/docs/current/logicaldecoding-explanation.html), [분할](https://www.postgresql.org/docs/current/ddl-partitioning.html), [ALTER TABLE](https://www.postgresql.org/docs/current/sql-altertable.html), [복제 설정](https://www.postgresql.org/docs/current/runtime-config-replication.html), [핫 스탠바이](https://www.postgresql.org/docs/current/hot-standby.html), [잠금](https://www.postgresql.org/docs/current/explicit-locking.html), [WAL 설정](https://www.postgresql.org/docs/current/runtime-config-wal.html), [시퀀스](https://www.postgresql.org/docs/current/sql-createsequence.html), [연속 보관](https://www.postgresql.org/docs/current/continuous-archiving.html), [논리 복제의 제약](https://www.postgresql.org/docs/current/logical-replication-restrictions.html), [pg_upgrade](https://www.postgresql.org/docs/current/pgupgrade.html), [연결 풀의 기능](https://www.pgbouncer.org/features.html).
- 공학 글: [PostgreSQL로 큐 만들기](https://brandur.org/postgres-queues), [LISTEN/NOTIFY가 커지지 않는 까닭](https://www.recall.ai/blog/postgres-listen-notify-does-not-scale), [무중단 스키마 변경의 어려운 점](https://gocardless.com/blog/zero-downtime-postgres-migrations-the-hard-parts/).

다시 확인할 것: 페이지는 요약을 거쳐 읽었고 인용의 글자 그대로는 확인하지 못했어요. `WAIT FOR LSN` 문서는 열리지 않았고, 그 기능은 미확인이에요. 통계 뷰와 함수 문서는 앞부분만 요약됐어요. 글이 말하는 이 분야의 최근 사건들은 그 글이 그렇게 말한다는 뜻이고 독립 확인을 하지 못했어요. WAL이 3~4배라는 것은 추정이에요(가정).

쓴 곳: 쓴 주체와 맡긴 사람의 기록, 위험의 등급과 정책에 의한 자동 병합, 병합 큐의 차례를 싸게 하는 것(ADR-0014). 처음 싣기를 나누기, 머리 행과 차례의 번호, 정족수 동기 복제, 연결 풀과 잠금의 규칙, 스키마 변경의 차례, `kg_id` 길잡이 층(ADR-0009, postgres-operations.md).

## 7. 질의 엔진을 만들까 살까 (ADR-0006, ADR-0010)

물음: 영향 범위를 답하는 엔진을 기성 그래프 엔진으로 할까 직접 만들까, 어느 언어로 할까.

확인한 것(요약을 거쳐 읽었어요. 일부는 미러나 2차 자료라 원문 대조가 필요해요):
- 그래프 엔진의 라이선스와 상태: [Kuzu 소개](https://www.puppygraph.com/blog/what-is-kuzudb), [Ryu 그래프](https://blog.ryugraph.io/posts/introducing-predictable-labs-and-ryugraph), [Memgraph](https://github.com/memgraph/memgraph)와 [BSL](https://raw.githubusercontent.com/memgraph/memgraph/master/licenses/BSL.txt), [FalkorDB 라이선스](https://docs.falkordb.com/license.html)와 [설계](https://docs.falkordb.com/design/), [Neo4j 라이선스](https://neo4j.com/licensing/?ref=blog), [Nebula](https://github.com/vesoft-inc/nebula), [TigerGraph 판 비교](https://www.tigergraph.com/comparison-of-tigergraph-editions/), [DuckPGQ](https://duckdb.org/community_extensions/extensions/duckpgq).
- 증분 계산과 규칙 엔진: [DBSP](https://www.vldb.org/pvldb/vol16/p1601-budiu.pdf), [differential dataflow](https://github.com/TimelyDataflow/differential-dataflow), [Feldera](https://github.com/feldera/feldera), [Soufflé](https://github.com/souffle-lang/souffle), [Skyframe](https://bazel.build/reference/skyframe.md).
- 규모의 사례: [Zanzibar](https://research.google/pubs/zanzibar-googles-consistent-global-authorization-system/), [LinkedIn LIquid](https://engineering.linkedin.com/blog/2020/liquid-the-soul-of-a-new-graph-database-part-1), [TAO](https://usenix.org/system/files/conference/atc13/atc13-bronson.pdf), [Batfish의 교훈](https://foci.uw.edu/papers/sigcomm23-batfish-lessons.pdf), [Uber의 의존 분석](https://www.uber.com/us/en/blog/automated-dependency-analysis/).
- 그래프 처리와 Go의 메모리: [GAP 벤치](https://ar5iv.arxiv.org/html/1508.03619), [Ligra](https://jshun.csail.mit.edu/ligra.pdf), [Go GC 안내](https://go.dev/doc/gc-guide), [Green Tea GC](https://go.dev/blog/greenteagc), [큰 힙의 GC 부담 줄이기](https://blog.gopheracademy.com/advent-2018/avoid-gc-overhead-large-heaps/).
- 벤치마크 방법: [LDBC SNB](https://arxiv.org/pdf/2001.02299), [그래프 벤치마크의 조사](https://arxiv.org/html/2404.00766v1).

찾지 못한 것: 우리와 가장 가까운 분야(자산과 서비스 지도의 영향 분석)의 공개된 수는 찾지 못했어요. 사례의 수는 요약 도구로 읽은 것이라 글자 그대로의 인용으로 쓰지 않았어요.

판단: 요구한 뜻(채널별 최소 고정점, 이중화 묶음, 세 값, 과거 버전)을 허용 라이선스로 함께 채우는 엔진이 없었어요. 그래서 직접 만들어요(ADR-0006 2번, query-engine-shape.md).

## 8. 규모별 비용의 측정 (ADR-0010, query-engine-shape.md)

같은 그래프를 Go와 Rust로 짠 엔진으로 쟀어요. 가정이에요(약속이 아니에요).

조건: 지어낸 모양(계층, 중복 구조, 팬아웃, 서비스 그래프를 내가 정한 인자로 만들었어요), 2 vCPU, 메모리 약 8GB, 단일 시드, 규모마다 한 번씩, 디스크 입출력 없음, 속성 거르기와 과거 버전과 읽기·쓰기 동시 접근 없음. 같은 바이너리를 두 번 돌리면 10~40% 편차가 있어요.

| 무엇 | 잰 값 |
|---|---|
| 규모 | 노드와 엣지 합 100만, 1천만, 3천만, 1억(1억에서 노드 약 2,630만, 엣지 약 7,400만) |
| 메모리 | 노드와 엣지 하나에 약 12바이트. 1억에서 짓고 난 뒤 상주 약 1.2GB(Go와 Rust가 같아요) |
| 짓기(1억) | Go 약 4.7~7.8초, Rust 약 3.8~4.0초 |
| 홀 하나가 서는 경우 | 가운데값 약 2ms(Go), 약 1.8ms(Rust). 영향받은 VM 약 3.4만 |
| 사이트의 전원 둘이 다 서는 경우 | 가운데값 약 6~7ms(Go), 약 5ms(Rust). 1억에서 최대 약 46ms와 37ms. 영향받은 VM 약 20만 |
| 질의 하나의 비용 | 건드린 노드 하나에 약 20~35ns |
| Go의 GC | 질의 구간에서는 한 번도 돌지 않았어요. 멈춤은 동시 질의의 작업자별 임시 할당과 갱신 단계에서만 생겼고 단일 최대 1ms 아래였어요 |
| 정확성 | 느리지만 뻔한 기준 구현과 1,367개 질의의 모든 노드 상태가 일치(불일치 0). Go와 Rust의 체크섬도 일치 |

한계: 합성 모양이라 실제 모양과 지연이 크게 달라질 수 있어요. 작은 장비에서 쟀고 동시성은 작업자 둘까지만 봤어요. 지연의 꼬리(최대)는 한 번의 운영체제 지터와 첫 접근의 페이지 폴트에 민감해요. Go가 이 장비에서 먼저 닿는 한계는 지연이나 GC가 아니라 짓기와 합치기의 순간 메모리(추정 2.4억~3억 단위)예요. 이 값들은 클로즈 베타에서 지어낸 세계 생성기로 다시 재서 고쳐요.

## 9. 정직하게 남기는 한계

- 이 조사는 공개 문서와 작은 시제품이 근거예요. 제품 책임자의 운영 환경에 대한 사실은 담지 않았어요(ADR-0003).
- 요약 도구를 거친 숫자와 제한 값은 구현 전에 원문으로 다시 봐요.
- 미확인으로 적은 것에 결정이 기대고 있다면 그 결정 기록에 열린 물음으로 남겼어요.
