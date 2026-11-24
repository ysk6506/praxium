---
title: ADR-0104 엑셀 행 파서와 XML 감싸개와 고정 한도 — 두 읽기 경로에 함께
kind: decision
status: active
enforced_by:
  - TLOCK001
  - GOARCH010
  - backend/internal/xlsx/xs_read_equal_test.go
  - backend/internal/xlsx/xs_changed_test.go
  - backend/internal/xlsx/xs_guard_test.go
  - backend/internal/xlsx/xs_limits_test.go
  - backend/internal/xlsx/xs_attack_test.go
  - backend/internal/ingest/xs_grid_test.go
touches_protected:
  - docs/product-specs/requirements-ingestion.md
  - harness/config/test-lock.json
updates_docs:
  - docs/product-specs/requirements-ingestion.md
  - docs/guides/ingest-definitions.md
  - docs/design-docs/ingestion.md
decision_hash: a5f796cd8a7a93e6
---
# ADR-0104 엑셀 행 파서와 XML 감싸개와 고정 한도 — 두 읽기 경로에 함께

[ADR-0103](0103-excel-sheets-read-row-by-row-port-scanner-and-limits.md)이 "뒤의 PR 2"로 남긴 몫을 구현하면서 정한 세부를 적는다. 0103의 결정은 바꾸지 않는다.

## 맥락

0103은 시트를 행마다 읽는 스캐너를 넣고, 셀의 값을 글로 바꾸는 일을 `adapter.xlsx`의 행 파서 하나에 두며 옛 `ReadWorkbook`이 그 위에 서도록 하기로 했다. 또 공통 XML 감싸개와 목록 한도를 옛 경로에도 건다고 했다. 이름과 값은 구현에서 정해야 했다. 지금의 코드는 `encoding/xml`의 토큰을 묶지 않아 수 킬로바이트의 파일(중첩 300만 단계, 목록 수백만 개, 글 하나를 조각으로 모으기)이 `ingest run`과 `ingest inspect`의 메모리를 모두 쓰게 할 수 있다.

## 결정

1. **행 파서(`rowparser.go`)가 두 경로의 읽기이다.** 값이 있는 행만 파일의 순서대로 하나씩 낸다. `Sheet.Sorted()`를 부르지 않는다(메서드는 남는다). 값이 있는 행의 번호가 앞 행 이하이거나 한 행에서 값이 있는 칸의 열이 앞 칸 이하이면 `invalid_value`로 멈춘다(`the rows of sheet "S" are not in order (row 2 after row 3)`, `the cells of row 2 of sheet "S" are not in order (column A after column B)`). 값이 없는 행과 칸은 순서에 들지 않는다. 시트의 병합 범위와 숨긴 열과 숨긴 행의 수는 행을 다 읽은 뒤에 시트에 넣는다.
2. **모든 XML 파트에 같은 감싸개(`xmlguard.go`)를 건다.** 감싸개는 바이트를 훑으며 태그, 글 한 덩이, 주석, CDATA, 처리 명령의 길이를 따로 세어 하나라도 1 MiB를 넘으면 `the workbook is damaged: in the part P, … : this limit is fixed`(`invalid_value`)로 멈춘다. 열린 요소가 64를 넘어도 멈춘다. 문서형 선언(DOCTYPE)과 XML 선언이 아닌 처리 명령은 거절한다. 주석과 CDATA의 끝을 찾는 상태는 시작 표지 뒤에서 새로 시작하므로 `<!-->`로 끝을 속일 수 없다.
3. **목록과 글의 고정 한도.** 값은 설계의 값이고 이름은 아래와 같다. 넘으면 `the workbook is over the safety limit NAME (VALUE) …; this limit is fixed`이다. 소스 정의의 `limits`로 바꿀 수 없다.

   | 이름 | 값 |
   |---|---|
   | `max_sheets` | 4,096 |
   | `max_sheet_name_bytes`, `max_sheet_state_bytes` | 255, 32 |
   | `max_relationships` | 10,000 |
   | `max_relationship_id_bytes`(시트의 `r:id`도), `max_relationship_type_bytes`, `max_relationship_target_bytes` | 64, 256, 512 |
   | `max_number_formats`, `max_number_format_bytes` | 4,096, 256 |
   | `max_styles` | 65,536 |
   | `max_col_elements` | 16,384 |
   | `max_merge_ranges` | 100,000 |

   `max_styles`와 `max_merge_ranges`는 0103이 뒤의 PR 3에서 `Limits`의 필드로 만들기로 한 이름이다. 이 변경은 값을 고정 값으로 먼저 걸고, PR 3이 필드로 옮길 때 같은 값을 쓴다.
4. **셀 하나의 글은 모으는 동안에도 묶는다.** 값의 글과 인라인 글과 공유 문자열은 조각(CDATA 여러 개)으로 모을 수 있어, 다 모은 뒤에 글자 수를 보면 이미 늦다. 모은 바이트가 글자 수 한도의 16배(글자 하나가 최대 4바이트이고 `_x0041_` 같은 이스케이프가 일곱 바이트이므로 정상 파일은 닿지 않는다)를 넘으면 `max_cell_chars` 오류이다.
5. **읽는 곳의 오류는 파일의 오류로 바꾸지 않는다.** `damaged`는 `*ingest.SourceError`와 맥락의 취소와 기한 오류를 그대로 돌려준다. 그러지 않으면 저장소의 일시적인 오류가 "파일이 깨졌다"(설정의 문제)로 알려진다. 바이트 읽기에서는 이 오류가 나지 않으므로 옛 경로의 동작은 같다.
6. **`ingest inspect`의 격자는 병합을 한 번만 쥔다.** 병합 범위를 덮는 행마다 복사하던 것을 첫 행으로 정렬한 색인으로 바꾼다. 어느 범위가 자리를 덮는지는 "시트에 적힌 순서의 첫 범위"로 그대로이다.
7. **같음은 기대값 파일로 본다.** 정상 파일의 답이 같음을 PR 1 병합 직후의 코드(8aecc37)로 만든 기대값으로 견준다. 손으로 쓴 파일 27개의 답 전체와, 시드가 정해진 무작위 파일 1,500개의 답의 SHA-256이다. 바뀌는 의미(1번)는 손으로 쓴 기대값이다.

## 대안과 버린 이유

- **순서가 어긋난 시트를 정렬해서 읽기**: 행을 모두 쥐어야 해서 스트리밍이 안 된다. 엑셀과 쓰기 도구는 순서대로 쓴다.
- **깊이를 파서 위에서 세기**: `encoding/xml`은 열린 요소마다 틀을 쥐므로 세기 전에 이미 메모리를 쓴다. 바이트에서 먼저 센다.
- **목록의 한도를 소스 정의로 열어 두기**: 소스 정의가 올리면 "한도 안의 어떤 입력도 256 MiB 아래"라는 0103의 목표가 깨진다.
- **감싸개를 새 경로에만 걸기**: 같은 코드를 두 경로가 쓰고, `ingest inspect`는 옛 경로에 남는다. 한 번에 막는다.

## 결과

- `adapter.xlsx`에 `rowparser.go`, `xmlguard.go`, `limits.go`. `core.ingest`의 `Inspect` 격자. 요구사항 REQ-805~810.
- 바뀌는 의미(두 경로): 순서가 어긋난 행과 같은 번호의 행과 어긋난 칸은 오류이다(옛 코드는 정렬하거나 한 행의 칸으로 읽었다). 중첩 64단계 위, 1 MiB 위의 토큰, DOCTYPE, 한도 위의 목록은 오류이다(옛 코드는 메모리가 다할 때까지 읽었다). 정상 파일의 답은 그대로이다.
- 잠긴 테스트는 바뀌지 않는다(`lockcmp`의 CHANGED 0).
