---
title: 조직 속성 — 표준 타입에 내 컬럼을 속성으로 더하는 법
kind: guide
status: active
follows:
  - docs/design-docs/adr/0050-org-properties-as-overlays-in-the-model.md
  - docs/design-docs/adr/0053-org-properties-command-line-and-start-wiring.md
covers:
  - backend/internal/model/overlay_file.go
  - backend/cmd/praxium/modelcmd.go
  - backend/cmd/praxium/modelplan.go
  - backend/cmd/praxium/modelrun.go
  - backend/cmd/praxium/modelload.go
---
# 조직 속성

자산 대장에는 표준 모델에 없는 컬럼이 있다. "관리 부서", "도입 연도", "유지보수 계약 번호" 같은 것이다. 이런 컬럼을 표준 타입(`PhysicalServer`, `Rack`)의 **조직 속성**으로 더하면 수집한 값이 표준 속성과 똑같이 타입 화면, 객체 상세, 조회의 필터에 나온다. 이 문서는 조직 속성 파일의 모든 필드, 이름과 값의 규칙, 처음부터 끝까지 따라 하는 예, 나중에 바꿀 때의 규칙을 적는다. 적힌 명령과 출력은 이 버전의 실행 파일로 직접 돌려 확인했다. 설계의 이유는 [ADR-0050](../design-docs/adr/0050-org-properties-as-overlays-in-the-model.md)과 [ADR-0053](../design-docs/adr/0053-org-properties-command-line-and-start-wiring.md)에 있다.

## 1. 전체 흐름

1. 조직 속성 파일(`org-model.json`)에 어느 타입에 어떤 속성을 더할지 적는다.
2. `praxium model check`로 검사한다. 아무것도 저장하지 않는다.
3. `praxium model apply`로 데이터 폴더에 저장한다.
4. 수집 매핑(`mapping.json`)의 `attribute`에 `org.이름`을 쓰고 `praxium ingest run -data-dir`로 수집한다.
5. `praxium local`, `praxium api serve -store file`이 폴더의 속성을 읽어 보여 준다.

조직 속성은 데이터 폴더의 `definitions.json`에 저장되고, 수집과 서버는 시작할 때 이 파일을 한 번 읽는다. **속성을 바꾼 뒤에는 서버를 다시 시작해야 반영된다**(7절). 데이터 폴더가 없는 실행(메모리)과 PostgreSQL 스토어의 서버는 표준 모델만 쓴다.

## 2. 파일 형식

```json
{
  "format": "praxium-org-model",
  "version": 1,
  "overlays": [
    {
      "type": "PhysicalServer",
      "properties": [
        { "name": "org.management_department", "value_type": "string", "display_names": { "ko": "관리 부서" } },
        { "name": "org.introduced_year", "value_type": "integer" }
      ]
    }
  ]
}
```

파일은 JSON이고 1MiB까지다. 모르는 필드는 오류다(오타를 막는다).

### 2.1 최상위

| 필드 | 필수 | 뜻 |
|---|---|---|
| `format` | 예 | 항상 `praxium-org-model` |
| `version` | 예 | 항상 `1` |
| `overlays` | 예 | 오버레이 목록. 오버레이 하나는 속성을 더할 대상 하나다 |

### 2.2 오버레이

| 필드 | 필수 | 뜻 |
|---|---|---|
| `type` | 둘 중 하나 | 표준 객체 타입의 이름(`PhysicalServer`)이나 별칭 |
| `trait` | 둘 중 하나 | 표준 특성의 이름. 특성에 더한 속성은 그 특성을 가진 모든 타입이 갖는다 |
| `properties` | 예 | 더할 속성의 목록(2.3). 오버레이 하나에 100개까지, 전체 1000개까지 |

같은 대상의 오버레이를 둘 쓰면 오류다. 대상이 없거나 시스템(`sys`) 타입이면 오류다. 대상이 표준에서 물러난(`deprecated`) 것이면 경고만 한다.

### 2.3 속성

| 필드 | 필수 | 기본값 | 뜻 |
|---|---|---|---|
| `name` | 예 | | 속성 이름(2.4). 수집 매핑과 조회에서 이 이름을 쓴다 |
| `value_type` | 예 | | 값 종류(2.5). `string`처럼 짧게 쓰거나 `vt.string`처럼 ID로 쓴다 |
| `unit` | 수량일 때 | | `value_type`이 `quantity`일 때만 필요하고, 그때만 쓴다(`kW`) |
| `enum_values` | 열거일 때 | | `value_type`이 `enum`일 때 허용하는 값의 목록. 값은 `snake_case`, 100개까지, 값 하나는 64자까지 |
| `sensitivity` | 아니오 | `open` | 민감 등급. `open`, `restricted`, `confidential`. 읽는 사람의 등급이 모자라면 값이 가려진다 |
| `change_class` | 아니오 | `slow` | 값이 바뀌는 빈도. `static`(거의 안 바뀜)이나 `slow`(가끔 바뀜)만 된다 |
| `status` | 아니오 | `active` | `experimental`, `active`, `deprecated`. 물러나게 하는 길은 6절 |
| `display_names` | 아니오 | | 언어별 표시 이름(`{"ko": "관리 부서"}`). 이름 하나는 100자까지 |
| `description` | 아니오 | | 설명. 1000자까지 |
| `aliases` | 아니오 | | 다른 이름. 같은 규칙(2.4). 이름을 바꾸면 옛 이름이 저절로 여기에 남는다(6절) |
| `id` | 아니오 | 저장할 때 채움 | 속성의 바뀌지 않는 ID(`pr.org.physical_server.management_department`). 처음 저장할 때 정해지고 이름을 바꿔도 그대로다. 손으로 쓰지 말고 `export`한 파일에서 가져온다 |
| `attr_no` | 아니오 | 저장할 때 채움 | 속성 번호. 저장소가 처음 저장할 때 100000부터 차례로 준다. 한 번 준 번호는 다시 쓰이지 않는다 |

다음 필드는 받지 않고, 쓰면 이유와 함께 오류다: `required`(채움률의 뜻이 정해질 때까지), `scope_key`, `history_kind`, `superseded_by`(조직 속성은 후속 없이 `deprecated`로만 물러난다), `change_class: dynamic`(흐름 수집이 생길 때까지).

### 2.4 이름 규칙

- `org.`로 시작하고, 뒤는 소문자와 숫자와 밑줄(`snake_case`)이다. 전체 64자까지다. 예: `org.management_department`, `org.maintenance_contract_no`.
- 표준 속성의 이름에는 점이 없으므로 표준이 나중에 어떤 속성을 더해도 조직 속성의 이름과 겹치지 않는다.
- 한 타입에서 이름과 별칭은 조상 타입과 특성의 속성까지 합쳐 겹치면 안 된다.
- 표준에 비슷한 속성이 있으면(이름의 밑줄을 빼면 같다) 경고한다. 오류는 아니다. 표준 속성으로 옮길 수 있는지 살펴 본다.

### 2.5 허용하는 값 종류

| `value_type` | 값 | 수집 매핑에서 칸의 모양 |
|---|---|---|
| `string` | 문자열 | 그대로 |
| `integer` | 정수 | `2021` |
| `number` | 숫자 | `3.5` |
| `boolean` | 참과 거짓 | `true`, `false`만 |
| `time` | 시각 | `2026-03-14T09:30:00Z`나 날짜만(`2026-03-14`, 그날 0시 UTC) |
| `enum` | 열거 | `enum_values`에 적은 값 그대로 |
| `quantity` | 수량 | 숫자. `unit`이 단위다 |
| `ipv4_address`, `mac_address`, `serial_number`, `fqdn` | 이름 붙은 문자열 | 형식을 검사하고 가지런히 한다 |

`ref`, `list`, `map`은 받지 않는다. 다른 객체를 가리키는 것은 링크 타입이 할 일이다.

### 2.6 한도

항목 본문 256KiB, 파일 1MiB, 오버레이 하나에 속성 100개, 전체 1000개, 열거 값 100개(값 하나 64자), 표시 이름 100자, 설명 1000자.

## 3. 명령

| 명령 | 하는 일 |
|---|---|
| `praxium model check -file f [-data-dir d]` | 저장하지 않고 검사한다. 오류는 전부 모아 한 번에, 그다음 경고와 저장된 것과의 차이(추가, 호환되는 변경, 호환성을 깨는 변경)를 보인다 |
| `praxium model apply -file f -data-dir d [-m 메시지]` | 검사에 통과하면 저장한다. 바뀐 것이 없으면 아무것도 쓰지 않는다 |
| `praxium model show -data-dir d [-set N]` | 저장된 조직 속성을 본다 |
| `praxium model export -data-dir d [-set N]` | 저장된 것을 `id`와 `attr_no`가 채워진 파일로 낸다. 다시 `apply`해도 바뀌는 것이 없다 |
| `praxium model history -data-dir d [-limit N]` | 정의 세트(`D-N`)를 새것부터 본다: 시각, 해시 앞 8자, 추가와 바뀐 속성 수, 메시지 |

- 모든 명령에 `--help`(사용 예가 있다)와 `--json`이 있다. `export`는 늘 JSON이다.
- 종료 코드: 0은 끝남(`check`는 파일이 괜찮음), 1은 실패(`check`는 오류나 호환성을 깨는 변경이 있음, `apply`는 거절), 2는 명령줄 오류다.
- `-store postgres`를 주면 "정의의 PostgreSQL 저장은 아직 없다"고 말하고 종료 코드 2로 끝난다.
- `apply`는 데이터 폴더의 잠금을 잡는다. 서버(`local`, `api serve`)나 `ingest run`이 폴더를 열고 있으면 이유를 말하고 거절한다. 서버를 멈추고 `apply`한 뒤 다시 시작한다. `check`, `show`, `export`, `history`는 잠금을 잡지 않아 서버가 도는 중에도 된다.
- 번호는 저장소의 번호 원장에서 받고, 번호 올리기와 새 정의 세트와 머리 쓰기는 한 번의 쓰기다. 도중에 멈춰도 옛 상태나 새 상태 하나만 보인다. 거절된 `apply`는 아무것도 쓰지 않는다.

## 4. 따라 하기: 자산 대장에 관리 부서와 도입 연도가 있을 때

빈 폴더에서 시작한다. 지어낸 자산 대장(`ledger.csv`)에 표준에 없는 컬럼 둘이 있다.

```
asset_id,serial_number,manufacturer,power_state,management_department,introduced_year
SV-3001,SN-C100,Zorvane,on,Infra Team 1,2021
SV-3002,SN-C200,Zorvane,on,Infra Team 2,2022
SV-3003,SN-C300,Quillon,off,Infra Team 1,2019
```

### 4.1 속성 파일을 쓰고 검사한다

`org-model.json`:

```json
{
  "format": "praxium-org-model",
  "version": 1,
  "overlays": [
    {
      "type": "PhysicalServer",
      "properties": [
        { "name": "org.management_department", "value_type": "string", "display_names": { "ko": "관리 부서" } },
        { "name": "org.introduced_year", "value_type": "integer", "display_names": { "ko": "도입 연도" } }
      ]
    }
  ]
}
```

```
$ praxium model check -file org-model.json
org-model.json: 1 overlays, 2 properties, compared with the standard model alone
PhysicalServer
  Added      org.introduced_year
  Added      org.management_department
ok: apply would store 2 properties added and 0 changed
```

### 4.2 저장한다

```
$ praxium model apply -file org-model.json -data-dir data -m "관리 부서와 도입 연도 추가"
stored D-1 (13b6ec31): 2 properties added, 0 changed
A running praxium reads the definitions when it starts: start praxium local or api serve again for the change to show.
$ praxium model apply -file org-model.json -data-dir data
nothing changed: the stored definitions (D-1) already say this
$ praxium model show -data-dir data
organization properties of D-1 (13b6ec31)
PhysicalServer
  org.introduced_year                integer   active     attr_no 100001  ko=도입 연도
  org.management_department          string    active     attr_no 100000  ko=관리 부서
```

괄호 안의 8자는 정의 세트의 해시 앞부분이다. 같은 내용은 같은 해시가 된다.

### 4.3 수집 정의를 쓰고 수집한다

`source.json`은 파일의 컬럼을 선언한다.

```json
{
  "id": "src_asset_ledger",
  "name": "asset ledger",
  "adapter": "file",
  "datasets": [
    {
      "id": "ds_ledger",
      "parser": "csv",
      "natural_key": ["asset_id"],
      "columns": [
        { "name": "asset_id" }, { "name": "serial_number" }, { "name": "manufacturer" },
        { "name": "power_state" }, { "name": "management_department" }, { "name": "introduced_year" }
      ]
    }
  ]
}
```

`mapping.json`은 컬럼을 속성에 잇는다. 조직 속성은 `org.`을 붙인 이름을 `attribute`에 쓴다.

```json
{
  "feeds": [
    {
      "id": "f_server",
      "dataset": "ds_ledger",
      "object_type": "PhysicalServer",
      "creates_objects": true,
      "retires_objects": true,
      "basis": "declared",
      "attributes": [
        { "attribute": "name", "column": "asset_id" },
        { "attribute": "manufacturer", "column": "manufacturer" },
        { "attribute": "serial_number", "column": "serial_number" },
        { "attribute": "power_state", "column": "power_state" },
        { "attribute": "org.management_department", "column": "management_department" },
        { "attribute": "org.introduced_year", "column": "introduced_year" }
      ],
      "identity": [
        {
          "components": [
            { "property": "manufacturer", "column": "manufacturer" },
            { "property": "serial_number", "column": "serial_number" }
          ],
          "priority": 1
        }
      ]
    }
  ]
}
```

식별 키(`identity`)에는 표준 속성만 쓴다. 조직 속성을 쓰면 이유를 말하고 거절한다(이미 저장된 객체의 식별을 바꾸면 이력을 잃는다). 나머지 필드는 [수집 정의 형식](ingest-definitions.md)에 있다.

```
$ praxium ingest run -data-dir data -source source.json -mapping mapping.json -input ds_ledger=ledger.csv
ingest source=src_asset_ledger run=1 objects=3 links=0 volatile=3 retired_objects=0 retired_links=0 changed=true new_version=true ...
  dataset ds_ledger collected=true received=3 unreadable=0 read=3 used=3 filtered=0 problem=0 unused=0 ...
completed without problems
```

`ingest run`은 폴더의 조직 속성을 시작할 때 읽는다. 속성 이름을 `org.` 없이 쓰면 "type PhysicalServer has no attribute "management_department"; the organization property of that name is org.management_department, write that"라고 알려 준다.

### 4.4 조회한다

```
$ praxium local -data-dir data
$ praxium --endpoint ... --token ... query find --type PhysicalServer --attr org.management_department.equals="Infra Team 1"
OBJECT_TYPE     COUNT
PhysicalServer  2

OBJECT_ID                             OBJECT_TYPE     DISPLAY_NAME
obj_2f3366066d1c7073b9015281ead4a1e5  PhysicalServer  SV-3001
obj_3c9c01bf9ccb0c28385bb1562f2edac9  PhysicalServer  SV-3003
```

`--attr 이름.연산=값`은 **마지막 점**에서 이름과 연산을 가른다. 그래서 점이 있는 조직 속성 이름도 그대로 쓴다. 연산은 `equals`, `in`(쉼표 목록), `prefix`, `range`(`2020..2023`)이고 `--attr`를 여러 번 쓰면 모두 맞는 것만 나온다. 객체 상세(`object get`)의 속성 목록에도 `org.management_department`와 `org.introduced_year`가 표준 속성과 나란히 나오고, 타입 응답(`types get PhysicalServer`)의 속성에도 있다. 서버를 다시 시작해도 같다.

## 5. 속성을 더한다

파일에 속성을 더하고 `check`, `apply`한다. 새 속성은 다음 번호를 받는다. 같은 타입에 더해도, 다른 타입이나 특성의 오버레이를 더해도 된다. 그 뒤에 매핑의 `attribute`에 새 이름을 쓰고 `ingest run`한다. 서버가 폴더를 열고 있으면 먼저 멈춘다(7절).

## 6. 바꾸는 규칙

파일은 **완전한 상태**를 말한다. 저장된 속성이 파일에 없으면 "빠진" 것이고 호환성을 깨는 변경이다. 그래서 바꿀 때는 `export`한 파일을 고쳐 `apply`한다.

```
praxium model export -data-dir data > org-model.json    # 고친다
praxium model check -file org-model.json -data-dir data
praxium model apply -file org-model.json -data-dir data -m "이유"
```

| 바꾸는 일 | 분류 | 어떻게 되나 |
|---|---|---|
| 속성을 더한다 | 추가 | 새 번호를 받는다 |
| 표시 이름, 설명을 바꾼다 | 호환 | 값과 조회에 영향이 없다 |
| 별칭이나 열거 값을 더한다 | 호환 | |
| 민감 등급을 올린다(`open`에서 `restricted`로) | 호환 | |
| `status`를 `deprecated`로 바꾼다 | 호환 | 물러나게 하기. 아래 |
| **이름을 바꾼다**(`export`한 파일에서 `id`를 그대로 두고 `name`을 고친다) | 호환 | 번호가 그대로라서 값이 따라온다. 옛 이름은 파일에 적지 않아도 `aliases`에 남고, 매핑과 조회에서 계속 통한다 |
| 값 종류, 단위, `change_class`를 바꾼다 | **호환성을 깨는 변경** | 거절한다 |
| 열거 값을 뺀다, 민감 등급을 낮춘다 | **호환성을 깨는 변경** | 거절한다 |
| 속성을 파일에서 뺀다 | **호환성을 깨는 변경** | 거절한다 |
| 별칭을 지운다 | 호환 | 옛 이름이 더는 안 통한다. 매핑에서 옛 이름을 쓰고 있으면 먼저 고친다 |

**물러나게 하기.** 조직 속성은 지우지 않는다. 행과 지난 버전이 번호로 속성을 가리키기 때문이다. `status: deprecated`로 바꾸면 새 매핑이 쓸 수 없게 되고, 이름과 번호는 계속 예약된다. 물러난 속성에 잇는 매핑은 정의 오류로 거절한다. 이미 쌓인 값은 읽을 수 있다. 원래대로 되돌리려면 `status`를 `active`로 되돌린다.

```
$ praxium ingest run -data-dir data -source source.json -mapping mapping.json -input ds_ledger=ledger.csv
praxium: feed f_server: attribute org.introduced_year is deprecated and nothing replaces it; remove it from the mapping, or ask whoever keeps the model to put it back to active
```

**호환성을 깨는 변경은 거절한다.** 강제하는 플래그는 없다. 거절과 가야 할 길은 이렇게 나온다.

```
$ praxium model apply -file break.json -data-dir data
break.json: 1 overlays, 2 properties, compared with the stored definitions D-3
PhysicalServer
  Breaking   org.introduced_year: the value type changes from vt.integer to vt.string: add a new property, fill it and deprecate the old one
not ok: 0 errors, 1 breaking changes; apply would refuse the file
praxium: nothing was stored: the file has 0 errors and 1 breaking changes
```

값 종류를 바꾸고 싶으면 **새 속성을 더하고, 새 속성에 값을 채우고(매핑을 새 속성으로 옮겨 수집한다), 옛 속성을 물러나게 한다.** 이 세 걸음이 번호와 지난 값을 지키는 길이다.

**이름 바꾸기 예.** `export`한 파일에서 `org.management_department`를 `org.owning_department`로 고치고 `apply`한다. 번호(`attr_no`)와 `id`는 그대로다.

```
$ praxium model check -file org-model.json -data-dir data
org-model.json: 1 overlays, 2 properties, compared with the stored definitions D-1
PhysicalServer
  Compatible org.owning_department: the name changes from org.management_department to org.owning_department and the old name stays as an alias
ok: apply would store 0 properties added and 1 changed
```

`history`는 각 세트가 무엇을 바꿨는지 보인다. 지난 세트의 내용은 `show -set N`으로 보고, 되돌리려면 `export -set N`으로 낸 파일을 `apply`한다(새 세트가 하나 더 생기고, 옛 세트는 그대로 남는다. 단, 되돌리는 변경도 위 표의 규칙을 따라서 호환성을 깨는 변경이면 거절된다).

```
$ praxium model history -data-dir data
D-3    2026-11-14T19:39:22Z  50eef347  +0 added, 1 changed  도입 연도를 물러나게 함
D-2    2026-11-14T19:39:16Z  692e40cc  +0 added, 1 changed  관리 부서를 소유 부서로 이름 바꿈
D-1    2026-11-14T19:39:15Z  13b6ec31  +2 added, 0 changed  관리 부서와 도입 연도 추가
```

## 7. 서버를 다시 시작해야 반영된다

`praxium local`, `praxium api serve -store file`은 시작할 때 폴더의 조직 속성을 읽고 도는 동안 바꾸지 않는다. 그래서 `apply`는 폴더를 연 프로세스가 있으면 거절한다. 순서는 이렇다.

1. 서버를 멈춘다(Ctrl+C).
2. `praxium model apply`.
3. `praxium ingest run -data-dir`로 새 속성의 값을 수집한다.
4. 서버를 다시 시작한다.

서버가 옛 정의로 도는 중에 새 번호의 값이 폴더에 들어오는 일(정의를 바꾸기 전에 시작한 서버와 그 뒤의 수집이 겹칠 때)이 있어도 서버는 그 값을 건너뛴다. 이름 없는 속성이 나오거나 서버가 죽지 않는다. 다시 시작하면 값이 보인다.

저장된 정의를 표준 모델과 합칠 수 없으면(예: 표준에서 대상 타입이 사라졌거나, `definitions.json`이 깨졌으면) 수집과 서버는 **시작하지 않는다.** 이유와 해결 방법을 한두 줄로 말한다.

```
praxium: cannot start: the organization properties stored in data do not fit the model (...). Look at them with praxium model show -data-dir data and apply a corrected file with praxium model apply; or move definitions.json away to start with the standard model alone
```

## 8. 검사가 알려 주는 오류

| 메시지의 일부 | 뜻과 고치는 법 |
|---|---|
| `has the name "management_department": want org. and snake_case` | 이름을 `org.`로 시작하고 소문자, 숫자, 밑줄로 쓴다 |
| `the overlay target "X" does not exist` | `type`이 표준 타입의 이름이 아니다. `praxium types list`로 이름을 본다 |
| `has the value type vt.list ... (a link type is the way to point at another object)` | `ref`, `list`, `map`은 받지 않는다. 문자열이나 열거로 바꾼다 |
| `a unit is needed for a quantity and only for a quantity` | 수량에만, 수량에는 꼭 `unit`을 쓴다 |
| `enum values are needed for an enum and only for an enum` | 열거에만, 열거에는 꼭 `enum_values`를 쓴다 |
| `has the enum value "Gold", which is not snake_case` | 열거 값을 소문자와 밑줄로 쓴다 |
| `is required` / `is a scope key` / `sets history_kind` | 지금은 받지 않는 필드다. 뺀다 |
| `the file has two overlays for ...` | 같은 대상의 오버레이를 하나로 합친다 |
| `looks like the standard property ...: consider moving to it` (경고) | 표준에 같은 뜻의 속성이 있다. 쓸지 정한다 |
| `the data folder ... is in use by another praxium` | 폴더를 연 서버나 수집을 멈추고 다시 한다 |

## 9. 지금의 한계

- 속성만 더한다. 새 객체 타입, 링크 타입, 식별 키, 열거 값 더하기(표준 속성에)는 아직 없다.
- 조직 속성은 식별 키에 쓸 수 없고, 흐름으로 들어오는 값(`dynamic`)과 필수(`required`)도 아직 못 쓴다.
- 정의는 데이터 폴더(내장 파일 스토어)에만 저장한다. PostgreSQL 저장은 아직 없다. PostgreSQL 스토어의 서버는 표준 모델만 쓴다.
- 쓰기는 이 명령 하나뿐이다. API로 쓰거나 제안과 승인을 거치는 흐름은 없다.
- 응답의 속성에는 아직 표시 이름과 출처(조직 속성인지)가 없고, 웹 화면은 이름(`org.management_department`)을 그대로 보인다.
- 서버를 다시 시작해야 반영된다(7절).
