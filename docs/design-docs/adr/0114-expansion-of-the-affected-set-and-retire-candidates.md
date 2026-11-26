---
title: ADR-0114 새 수집 흐름의 영향 집합 확장과 퇴역 후보
kind: decision
status: active
enforced_by:
  - GOARCH010
  - TLOCK001
  - backend/internal/ingest/exp_run_test.go
  - backend/internal/pgingest/exp_rules_test.go
  - backend/internal/pgingest/exp_parity_test.go
  - backend/internal/pgingest/exp_sources_test.go
  - backend/internal/pgingest/exp_scale_test.go
touches_protected:
  - harness/config/test-lock.json
  - docs/design-docs/adr/0108-identify-step-slots-rounds-and-groups-in-sql.md
  - docs/product-specs/requirements-ingestion.md
updates_docs:
  - docs/design-docs/ingestion-at-scale.md
  - docs/design-docs/ingestion-at-scale-identity.md
  - docs/product-specs/requirements-ingestion.md
amends:
  - adr: "0108"
    what: 결정 1의 피드 표(`IdentifyFeed`)에 `Retires`를 더하고, 3단계의 끝내기가 `record_ref.retirable`을 그 표지로 쓰는 것
decision_hash: 5afc9c2802c3c747
amended_by:
  - adr: "0118"
    what: 결정 3(퇴역 후보의 조건 2와 3, 표의 "다른 소스의 레코드가 서 있는 객체" 행), 결정 5(스키마를 바꾸지 않는다, 생성기 7), 결정 6(읽는 양, 단위 안에서만 한 번)과 알려진 한계(단위를 넘어 같은 키를 다시 읽는다, 진 링크 문구), 결정 1의 `ExpandStart`
---
# ADR-0114 새 수집 흐름의 영향 집합 확장과 퇴역 후보

## 맥락

ADR-0108이 3단계(식별)까지 했다. 이 결정은 그 다음, 영향 집합을 **넓히는** 일이다(식별과 영향 집합 문서 3절의 규칙 (다)에서 (사)). 4단계(다시 계산)는 하지 않는다. 식별이 끝난 실행의 레코드(`calc_record`)에서 출발해, 같이 다시 계산해야 하는 레코드와 객체를 `calc_record`와 `calc_object`에 더하고, 어떤 객체를 퇴역시킬 후보로 볼지를 한 곳에서 정한다. 옛 수집(`ingest.Runner`)은 그대로 둔다.

## 결정

1. **포트는 `core.ingest`의 `Expander`이고 구현은 `adapter.pgingest`의 SQL이다.** `Identifier`와 같은 모양이다. 모든 호출이 임대의 시도 번호를 대고(읽기도 그렇다), 시도가 다르면 `CodeRunLeaseLost`로 거절하며 아무것도 바꾸지 않는다.

   | 메서드 | 하는 일 |
   |---|---|
   | `BeginExpand(lease, start)` | 계산 테이블의 주인이 이 실행인지 확인하고 `calc_job`을 확장(3)으로 바꾸고 자리를 처음에 놓는다. 식별이 끝나지 않았거나 이미 시작한 실행은 `CodeRunStage`로 거절한다 |
   | `ExpandStep(lease)` | 자리가 가리키는 단위 하나를 한다. 단위의 쓰기와 자리의 기록이 한 트랜잭션이다 |
   | `ExpandPlace(lease)` | 시작했는지, 끝났는지, 어느 규칙의 몇 번째 단위인지 |
   | `ExpandReport(lease)` | 센 수(퇴역 후보, 퇴역 객체의 링크의 레코드, 키가 같은 레코드, 한도 때문의 레코드, 주장 객체) |

   `RunExpand`가 위를 이어 돌린다. `ExpandStart`는 전체 계산(`Full`), 한 단위의 크기(`Rows`, 기본 `DefaultExpandRows` = 2000), 한도가 있는 링크 유형(`Cards`, 모델에서 `ExpandCards`가 만든다)을 준다. 음수 크기나 같은 링크 유형의 중복은 `CodeInvalidValue`다. 이 포트에는 Go에 일을 넘기는 쪽(`Put`과 `Page`)이 없다. 모든 규칙이 SQL이라서 "같은 단위를 두 번 부르면 거절한다"는 확인은 해당하지 않는다. 같은 단위의 이어 부름은 단위가 한 트랜잭션이라 같은 결과를 낸다.

2. **규칙마다 이름 붙은 단위가 하나다.** 각 규칙은 레코드(또는 객체) 번호의 범위를 읽어 `calc_record`와 `calc_object`에 넣기만 한다(`ON CONFLICT DO NOTHING`). 규칙은 순서대로 한 번씩 돈다. 마지막 규칙의 마지막 단위 전에는 `Finished`가 거짓이다.

   | 규칙 | 이름 | 하는 일 | 문장 |
   |---|---|---|---|
   | (다) | `old` | 이 실행의 레코드가 전에 묶였던 객체(식별 줄의 `record_ref`)를 `calc_object`에 넣고, 퇴역 후보는 이유 `retire`(4)로, 아니면 `oldref`(2)로 | `expOldSQL` |
   | (사) | `retire_out`, `retire_in` | 퇴역 후보 객체에서 나가고 들어오는 링크를 만든 레코드를 다시 읽는다 | `retireOutSQL`, `retireInSQL` |
   | (라) | `key_old`, `key_new` | 이 실행의 레코드가 전에 가졌던(참조) 또는 지금 주는(`calc_end`) 링크 끝 키를 시작으로 가진 레코드를 읽는다 | `expKeyOldSQL`, `expKeyNewSQL` |
   | (마) | `limit_out`, `limit_in` | 한쪽이 하나인 링크 유형에서 같은 시작(또는 끝)을 다투는 링크의 레코드를 읽는다 | `expLimitOutSQL`, `expLimitInSQL` |
   | (바) | `claim` | 이 실행의 레코드가 주장하는 식별자(전에 것과 지금 것)를 주장하는 다른 객체를 `calc_object`(이유 3)에 넣는다 | `expClaimSQL` |

   전체 계산(`Full`)은 규칙 `all` 하나로 `raw_record`의 모든 열린 레코드를 이 실행의 레코드로 만든다. 이때도 퇴역 후보는 같은 판단(`expRetirable`)으로 정한다. 규칙은 나눈 것이므로 테스트 갈고리(`Store.expTest`)가 이름으로 규칙을 건너뛰게 할 수 있다. 여덟 개의 길을 하나씩 건너뛰면 고정된 사례가 하나씩 어긋난다. 모형 검사가 놓친 세 가지(주장 경쟁자, 퇴역 링크, 보낸 객체의 닫힘)가 먼저다.

3. **퇴역 후보를 판단하는 곳은 한 군데(`expRetirable`)다.** 객체가 후보인 조건은 셋이다.
   1. 이 실행의 레코드가 그 객체에 묶이지 않았다(식별이 `calc_object`에 이유 `identified`로 쓰지 않았다).
   2. 이 실행의 밖에 있거나 다시 읽기만 하는 열린 레코드가 가진 해결된 참조가 없다. 바뀌거나 닫힌 레코드의 참조는 지금 서 있는 것이 아니다.
   3. 남은 참조가 `retirable`이라고 말한다. 이 표지는 3단계의 끝내기가 레코드의 피드가 객체를 만들고 퇴역시킬 수 있을 때(`creates_objects`와 `retires_objects`)만 쓴다(`IdentifyFeed.Retires`).

   | 다른 점 | 옛 | 새 | 이유 |
   |---|---|---|---|
   | `retirable`이 서는 때 | 퇴역시키는 피드가 한 번 본 객체는 영원히 | 실행이 시작할 때 있던 참조가 말하는 것(서 있는 레코드의 피드) | 옛 표지는 상태를 실행 밖(메모리)에 쥐었다. 새 흐름은 참조에서만 읽어 이어받은 서버도 같은 판단을 한다 |
   | 다른 소스의 레코드가 서 있는 객체 | 그 소스의 행이 살아 있으면 지지가 있다 | 같다(해결된 참조가 있으면 후보가 아니다) | 같은 결과다 |
   | 지지가 없고 표지가 없는 객체 | 퇴역하지 않는다 | 후보가 아니다 | 같은 결과다 |

   옛 흐름과 같은 수집 사례(`idn_parity_more`의 입력, 13시간 간격의 일곱 실행)에서 후보는 옛 `RetireObjects`가 퇴역시키는 객체이고, 단계마다 살아 있는 객체의 집합과 퇴역한 수가 옛 `Runner`와 같다.

4. **`calc_object`는 첫 이유가 이긴다.** 같은 객체가 여러 규칙에 걸려도 줄은 하나이고 먼저 쓴 이유가 남는다(기본키 `obj_no`, `ON CONFLICT DO NOTHING`). 순서는 위 표의 순서로 고정이라 결과가 정해진다. 규칙의 합은 집합이라서 단위의 크기(1, 3, 기본)와 받은 행과 레코드 번호의 순서에 상관없이 같다.

5. **스키마를 바꾸지 않는다(생성기 버전은 그대로 7이다).** 새 줄은 `calc_job`의 값(`CalcJobExpand` = 3)과 `calc_object`의 이유(`oldref`, `claim`, `retire`)의 값뿐이다. `calc_bind`와 `calc_ref`에는 객체 번호의 인덱스가 없어서, 새 쪽의 지지는 `calc_object`의 식별 줄로 읽는다. 옛 쪽의 지지는 `record_ref_target`과 `calc_record`의 키로 읽는다. 인덱스가 더 필요하면 I-5b의 생성기 올리기에서 한다.

6. **읽는 양은 레코드 수 m에 비례한다.** 단위가 읽는 행은 단위에 든 레코드의 수에 비례한다. 단위 안에서 레코드들이 공유하는 키, 객체, 주장은 한 번만 읽는다(DISTINCT로 줄인 CTE). 인덱스(`record_ref_target`, `record_ref_link`, `record_ref_lookup`, `link_src`, `link_dst`)로 읽는다.

7. **이어 가기는 단위 단위다.** 자리(`sync_run.counts`의 `expand`)와 단위의 쓰기가 한 트랜잭션이라 끊긴 단위는 아무것도 남기지 않고, 이어받은 서버가 같은 자리에서 같은 단위를 한다. 임대를 잃은 서버는 읽기를 포함해 모든 메서드에서 거절된다.

## 이유

- 규칙마다 이름과 문장을 나누면 건너뛰는 테스트가 가능하고, 규칙 하나가 틀리면 어느 것인지 테스트가 말해 준다(모형 검사가 통과시킨 세 가지가 실제로 있었다).
- 후보의 판단을 한 식으로 두면 4단계가 그 표지만 읽으면 되고, 옛 흐름과의 차이를 표 하나로 말할 수 있다.
- 읽을 것을 더 읽는 것(넓히기)은 안전하고 덜 읽는 것은 틀린 결과다. 규칙은 상위 집합 쪽으로 틀린다.

## 대안과 기각 사유

- **`calc_bind`와 `calc_ref`에 객체 번호의 인덱스를 더한다.** 생성기를 올려야 하고 이 결정의 범위(더하지 않는다)를 넘는다. 식별 줄(`calc_object`)로 같은 판단이 된다.
- **`retirable`을 객체에 영구 표지로 둔다(옛 방식).** 이어받은 서버가 알 수 없다.

## 알려진 한계

- 단위를 넘어 같은 키를 다시 읽는다. 키 하나를 레코드 m개가 공유하면 단위마다 한 번씩 읽는다. `calc_end`의 (`side`, `def_no`, `lookup_hash`) 인덱스가 있으면 없어진다. I-5b의 생성기 올리기에서 한다.
- 한도(카디널리티)에서 진 링크(`link` 테이블에 없는 것)를 다시 살리는 규칙은 이 결정이 하지 않는다. 지는 쪽은 4단계가 다시 계산할 때 비교할 수 있도록 상대를 읽어 오지만, 진 줄의 부활은 4단계 이후의 몫이다.
- 규칙이 `ON CONFLICT`로 넣으므로 같은 줄을 둘이 넣는 경쟁은 테스트에서 하나의 서버만 다룬다.

## 결과

- 4단계(다시 계산)는 `calc_object`와 `calc_record`를 읽어 일을 한다. 퇴역 후보는 `calc_object`의 이유 `retire`다.
- 상태 기계(I-7)는 `RunIdentify` 다음에 `RunExpand`를 부르면 된다.
