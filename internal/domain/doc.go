// Package domain is the inner core of the hexagonal architecture. It
// holds the value types (Project, Task, Comment, Event, Workflow, Tag,
// Law, Persona, Skill, Priority, Severity, …) and pure logic that
// operates on them — coded errors, workflow permission resolution,
// priority/severity registries — without importing any adapter or
// service package. Every other internal/* package may import domain;
// domain imports nothing from internal/.
package domain
