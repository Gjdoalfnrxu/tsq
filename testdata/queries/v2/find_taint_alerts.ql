import tsq::taint

from TaintAlert alert
select alert.getSrcKind() as "srcKind", alert.getSinkKind() as "sinkKind"
