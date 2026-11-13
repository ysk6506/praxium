package worldgen

import "strconv"

const pageSize = 100

// vmBuf collects a VM and its guest, NICs and disks, which follow it.
type vmBuf struct {
	id, name, uuid, vcpu, mem, guest string
	hostname                         string
	nics                             []string
	disks                            []string
}

// pager writes the pages of a JSON API source.
type pager struct {
	base          string
	f             *outFile
	page, n       int
	items, missed int64
}

// virtState is the JSON API source: pages of VMs and of hosts.
type virtState struct {
	vm    *vmBuf
	vms   pager
	hosts pager
}

func (p *pager) name() string { return "virt/" + p.base + "-" + pad4(p.page) + ".json" }

func pad4(n int) string {
	s := strconv.Itoa(n)
	for len(s) < 4 {
		s = "0" + s
	}
	return s
}

// add writes one item (a JSON object) into the current page, starting a new one when full.
func (p *pager) add(v *Views, item []byte) {
	if p.f == nil || p.n == pageSize {
		p.closePage(v, false)
		p.page++
		p.f = v.fs.open(p.name(), SrcVirt, "json")
		p.f.write([]byte(`{"page":` + strconv.Itoa(p.page) + `,"page_size":` + strconv.Itoa(pageSize) + `,"items":[`))
		p.n = 0
	}
	if p.n > 0 {
		p.f.write([]byte{','})
	}
	p.f.write(item)
	p.n++
	p.items++
}

// closePage ends the page. last says that no page follows.
func (p *pager) closePage(v *Views, last bool) {
	if p.f == nil {
		return
	}
	next := "null"
	if !last {
		next = `"` + p.base + "-" + pad4(p.page+1) + `.json"`
	}
	tail := `],"next":` + next
	if last {
		honest := !v.p.Imperfections.FalseCompleteness
		complete := !(honest && p.missed > 0)
		tail += `,"complete":` + strconv.FormatBool(complete) + `,"total":`
		if honest {
			tail += strconv.FormatInt(p.items, 10)
		} else {
			tail += strconv.FormatInt(p.items+p.missed, 10)
		}
	}
	p.f.write([]byte(tail + "}\n"))
	p.f = nil
}

// miss counts a row the source drops. With FalseCompleteness the last page
// still claims a complete delivery and a total that includes the dropped rows.
func (p *pager) miss(v *Views, id string, forced bool) {
	p.missed++
	why := "the object is absent from this source only; it must not be treated as deleted"
	if forced && v.p.Imperfections.FalseCompleteness {
		v.log.add(Imperfection{"false_completeness", SrcVirt, p.base + "-*.json", id, "the last page claims complete=true and a total that counts this row; completeness must not be trusted and no deletion may be inferred"})
		return
	}
	v.log.add(Imperfection{"missing_row", SrcVirt, p.base + "-*.json", id, why})
}

func (s *virtState) finish(v *Views) {
	s.flushVM(v)
	s.vms.closePage(v, true)
	s.hosts.closePage(v, true)
}

func (s *virtState) startVM(v *Views, o *Object) {
	s.vm = &vmBuf{id: o.ID, name: o.str("name"), uuid: identOf(o, "is.vm_instance_uuid"), vcpu: o.str("vcpu_count"), mem: o.str("memory_gib"),
		guest: o.str("guest_os_family")}
}

func identOf(o *Object, scheme string) string {
	for _, x := range o.IDs {
		if x.scheme == scheme {
			return x.value
		}
	}
	return ""
}

func (s *virtState) child(o *Object) {
	if s.vm == nil {
		return
	}
	switch o.T.idx {
	case tOS:
		s.vm.hostname = o.str("hostname")
	case tVNic:
		s.vm.nics = append(s.vm.nics, `{"index":`+jsonNum(o.str("device_index"))+`,"mac":`+jsonStr(o.str("mac_address"))+`}`)
	case tVDisk:
		s.vm.disks = append(s.vm.disks, `{"unit":`+jsonNum(o.str("unit_number"))+`,"capacity_gb":`+jsonNum(o.str("capacity_gb"))+`}`)
	}
}

func jsonStr(s string) string {
	if s == "" {
		return "null"
	}
	return string(appendJSONString(nil, s))
}

func jsonNum(s string) string {
	if s == "" {
		return "null"
	}
	return s
}

// flushVM writes the collected VM as a nested item.
func (s *virtState) flushVM(v *Views) {
	m := s.vm
	if m == nil {
		return
	}
	s.vm = nil
	d := v.draw(SrcVirt, m.id)
	forced := s.vms.items+s.vms.missed == 6
	if forced && v.p.Imperfections.FalseCompleteness || d.chance(v.p.Imperfections.MissingRowRatio) {
		s.vms.miss(v, m.id, forced)
		return
	}
	hostKey := "host"
	page, fresh := s.vms.page, s.vms.f == nil || s.vms.n == pageSize
	if fresh {
		page++
	}
	if page >= 2 && v.p.Imperfections.ShapeDrift {
		hostKey = "hypervisor"
		if page == 2 && fresh {
			v.log.add(Imperfection{"shape_drift", SrcVirt, "vms-0002.json", "host", "from this page the field host is named hypervisor; a reader maps it by alias or quarantines it, and does not read the placement as empty"})
		}
	}
	b := append(v.buf[:0], `{"vm_id":`...)
	b = appendJSONString(b, srcID(SrcVirt, "vmo", m.id))
	b = append(b, `,"instance_uuid":`...)
	b = append(b, jsonStr(m.uuid)...)
	b = append(b, `,"name":`...)
	b = append(b, jsonStr(m.name)...)
	b = append(b, `,"`+hostKey+`":{"host_id":`...)
	b = appendJSONString(b, srcID(SrcVirt, "hyp", parentID(m.id)+"/hyp0"))
	b = append(b, `},"cpu":`...)
	b = append(b, jsonNum(m.vcpu)...)
	b = append(b, `,"memory_gib":`...)
	b = append(b, jsonNum(m.mem)...)
	b = append(b, `,"guest":{"family":`...)
	b = append(b, jsonStr(m.guest)...)
	b = append(b, `,"hostname":`...)
	b = append(b, jsonStr(m.hostname)...)
	b = append(b, `},"nics":[`...)
	b = appendJoined(b, m.nics)
	b = append(b, `],"disks":[`...)
	b = appendJoined(b, m.disks)
	b = append(b, `],"tags":["`...)
	b = append(b, tagOf(m.id)...)
	b = append(b, `"]}`...)
	s.vms.add(v, b)
	v.Rows["virt/vms"]++
	if d.chance(v.p.Imperfections.DuplicateKeyRatio) {
		s.vms.add(v, b)
		v.log.add(Imperfection{"duplicate_key", SrcVirt, s.vms.name(), m.id, "the same vm_id appears twice; one object results"})
	}
}

func tagOf(id string) string { return "t" + base32(hash64(id), 3) }

func appendJoined(b []byte, items []string) []byte {
	for i, it := range items {
		if i > 0 {
			b = append(b, ',')
		}
		b = append(b, it...)
	}
	return b
}

// host writes a hypervisor host; its server (the one made just before) gives the serial.
func (s *virtState) host(v *Views, o *Object, srv srvInfo) {
	d := v.draw(SrcVirt, o.ID)
	if d.chance(v.p.Imperfections.MissingRowRatio) {
		s.hosts.miss(v, o.ID, false)
		return
	}
	b := append(v.buf[:0], `{"host_id":`...)
	b = appendJSONString(b, srcID(SrcVirt, "hyp", o.ID))
	b = append(b, `,"name":`...)
	b = append(b, jsonStr(o.str("hostname"))...)
	b = append(b, `,"hardware":{"serial":`...)
	b = append(b, jsonStr(srv.serial)...)
	b = append(b, `},"connection":`...)
	b = append(b, jsonStr(o.str("connection_state"))...)
	b = append(b, '}')
	s.hosts.add(v, b)
	v.Rows["virt/hosts"]++
}
