package grammars

/*
#cgo LDFLAGS: -ldl
#include <dlfcn.h>
#include <stdlib.h>

typedef const void *(*ts_language_fn)(void);

static const void *dth_call_language(void *fn) { return ((ts_language_fn)fn)(); }
*/
import "C"

import (
	"fmt"
	"unsafe"
)

// loadLanguageSymbol dlopens a grammar shared library and calls its language constructor. Libraries stay
// loaded for the life of the process (grammars are registered once at startup).
func loadLanguageSymbol(path, symbol string) (unsafe.Pointer, error) {
	cpath := C.CString(path)
	defer C.free(unsafe.Pointer(cpath))
	handle := C.dlopen(cpath, C.RTLD_NOW|C.RTLD_LOCAL)
	if handle == nil {
		return nil, fmt.Errorf("dlopen %s: %s", path, C.GoString(C.dlerror()))
	}
	csym := C.CString(symbol)
	defer C.free(unsafe.Pointer(csym))
	fn := C.dlsym(handle, csym)
	if fn == nil {
		return nil, fmt.Errorf("symbol %s not found in %s", symbol, path)
	}
	lang := C.dth_call_language(fn)
	if lang == nil {
		return nil, fmt.Errorf("%s returned a nil language", symbol)
	}
	return unsafe.Pointer(lang), nil
}
