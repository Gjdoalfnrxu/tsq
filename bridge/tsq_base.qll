/**
 * Base bridge library for tsq.
 * Maps the structural fact relations (Node, File, Contains, SchemaVersion)
 * to QL-visible classes.
 */

/** An AST node in the TypeScript source. */
class ASTNode extends @node {
    ASTNode() { Node(this, _, _, _, _, _, _) }

    /** Gets the file containing this node. */
    File getFile() { Node(this, result, _, _, _, _, _) }

    /** Gets the syntactic kind of this node (e.g. "CallExpression"). */
    string getKind() { Node(this, _, result, _, _, _, _) }

    /** Gets the start line (1-based). */
    int getStartLine() { Node(this, _, _, result, _, _, _) }

    /** Gets the start column (0-based). */
    int getStartCol() { Node(this, _, _, _, result, _, _) }

    /** Gets the end line (1-based). */
    int getEndLine() { Node(this, _, _, _, _, result, _) }

    /** Gets the end column (0-based). */
    int getEndCol() { Node(this, _, _, _, _, _, result) }

    /** Gets a textual representation of this node. */
    string toString() { result = this.getKind() }
}

/** A source file in the extraction database. */
class File extends @file {
    File() { File(this, _, _) }

    /** Gets the file path. */
    string getPath() { File(this, result, _) }

    /** Gets the content hash. */
    string getContentHash() { File(this, _, result) }

    /** Gets a textual representation of this file. */
    string toString() { result = this.getPath() }
}

/** A parent-child containment relationship between AST nodes. */
class Contains extends @contains {
    Contains() { Contains(this, _) }

    /** Gets the parent node. */
    ASTNode getParent() { result = this }

    /** Gets the child node. */
    ASTNode getChild() { Contains(this, result) }
}

/** The schema version of the extraction database. */
class SchemaVersion extends @schema_version {
    SchemaVersion() { SchemaVersion(this) }

    /** Gets the version number. */
    int getVersion() { result = this }
}
