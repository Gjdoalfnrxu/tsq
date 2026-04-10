/**
 * Bridge library for module-related relations.
 * Maps ImportBinding, ExportBinding.
 */

/** An import binding (import { x } from "module"). */
class ImportBinding extends @import_binding {
    ImportBinding() { ImportBinding(this, _, _) }

    /** Gets the local symbol. */
    int getLocalSym() { result = this }

    /** Gets the module specifier string. */
    string getModuleSpec() { ImportBinding(this, result, _) }

    /** Gets the imported name (or "default" / "*"). */
    string getImportedName() { ImportBinding(this, _, result) }

    /** Gets a textual representation. */
    string toString() { result = this.getImportedName() + " from " + this.getModuleSpec() }
}

/** An export binding (export { x }). */
class ExportBinding extends @export_binding {
    ExportBinding() { ExportBinding(this, _, _) }

    /** Gets the exported name. */
    string getExportedName() { result = this }

    /** Gets the local symbol. */
    int getLocalSym() { ExportBinding(this, result, _) }

    /** Gets the file containing this export. */
    File getFile() { ExportBinding(this, _, result) }

    /** Gets a textual representation. */
    string toString() { result = this.getExportedName() }
}
