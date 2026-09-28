// Package field is the leaf for typed-in values: a Line (one prompt) or
// an Area (a textarea in bordered chrome). Conceptually Lines 1..n —
// one row, or many. Presentation props in, bytes out. No domain,
// config, app or sqlite.
//
// They share a concept and are not one state machine, the same lesson
// as list having Cards/Window/Picker/Viewport. Line takes a textinput
// by value and always paints the accent border; Area takes a textarea
// and a focus flag, and Resize must run on the persistent model so the
// field does not vanish after the first keystroke.
package field
