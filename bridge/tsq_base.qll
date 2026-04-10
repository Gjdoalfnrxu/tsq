/**
 * Base bridge library for tsq.
 * Maps the structural fact relations (Node, File, Contains, SchemaVersion)
 * to QL-visible classes.
 */

/** An AST node in the TypeScript source. */
class ASTNode extends @node {
    ASTNode() { node(this, _, _, _, _, _, _) }

    /** Gets the file containing this node. */
    File getFile() { node(this, result, _, _, _, _, _) }

    /** Gets the syntactic kind of this node (e.g. "CallExpression"). */
    string getKind() { node(this, _, result, _, _, _, _) }

    /** Gets the start line (1-based). */
    int getStartLine() { node(this, _, _, result, _, _, _) }

    /** Gets the start column (0-based). */
    int getStartCol() { node(this, _, _, _, result, _, _) }

    /** Gets the end line (1-based). */
    int getEndLine() { node(this, _, _, _, _, result, _) }

    /** Gets the end column (0-based). */
    int getEndCol() { node(this, _, _, _, _, _, result) }

    /** Gets a textual representation of this node. */
    string toString() { result = this.getKind() }
}

/** A source file in the extraction database. */
class File extends @file {
    File() { file(this, _, _) }

    /** Gets the file path. */
    string getPath() { file(this, result, _) }

    /** Gets the content hash. */
    string getContentHash() { file(this, _, result) }

    /** Gets a textual representation of this file. */
    string toString() { result = this.getPath() }
}

/** A parent-child containment relationship between AST nodes. */
class Contains extends @contains {
    Contains() { contains(this, _) }

    /** Gets the parent node. */
    ASTNode getParent() { result = this }

    /** Gets the child node. */
    ASTNode getChild() { contains(this, result) }
}

/** The schema version of the extraction database. */
class SchemaVersion extends @schema_version {
    SchemaVersion() { schema_version(this) }

    /** Gets the version number. */
    int getVersion() { result = this }
}
