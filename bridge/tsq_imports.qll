/**
 * Bridge library for module-related relations.
 * Maps ImportBinding, ExportBinding.
 */

/** An import binding (import { x } from "module"). */
class ImportBinding extends @import_binding {
    ImportBinding() { import_binding(this, _, _) }

    /** Gets the local symbol. */
    int getLocalSym() { result = this }

    /** Gets the module specifier string. */
    string getModuleSpec() { import_binding(_, result, _) and import_binding(this, _, _) }

    /** Gets the imported name (or "default" / "*"). */
    string getImportedName() { import_binding(_, _, result) and import_binding(this, _, _) }

    /** Gets a textual representation. */
    string toString() { result = this.getImportedName() + " from " + this.getModuleSpec() }
}

/** An export binding (export { x }). */
class ExportBinding extends @export_binding {
    ExportBinding() { export_binding(this, _, _) }

    /** Gets the exported name. */
    string getExportedName() { result = this }

    /** Gets the local symbol. */
    int getLocalSym() { export_binding(_, result, _) and export_binding(this, _, _) }

    /** Gets the file containing this export. */
    File getFile() { export_binding(_, _, result) and export_binding(this, _, _) }

    /** Gets a textual representation. */
    string toString() { result = this.getExportedName() }
}
