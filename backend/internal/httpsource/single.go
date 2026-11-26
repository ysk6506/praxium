package httpsource

// object reads the rest of an object whose opening has been read, as the one record of the answer (single). It is as big as a record
// may be and no bigger.
func (w *walker) object(start int64) error {
	obj := map[string]any{}
	for w.dec.More() {
		keyTok, err := w.dec.Token()
		if err != nil {
			return err
		}
		key, _ := keyTok.(string)
		var v any
		if err := w.dec.Decode(&v); err != nil {
			return err
		}
		obj[key] = v
		// The floor is not moved while the object is read: the window refuses to give more than a record may be (and its slack) from
		// where the object began, so that an object with very many keys is cut at the size of a record and not read to its end.
	}
	if _, err := w.dec.Token(); err != nil {
		return err
	}
	w.out.n++
	if w.dec.InputOffset()-start > recordMaxBytes {
		return errRecordTooBig
	}
	more := true
	if w.skip == 0 {
		more = w.each(item{rec: obj, index: 0, last: true, whole: true})
	}
	if !more {
		w.out.stopped = true
		return nil
	}
	w.valueDone()
	return nil
}
