package platformserver

// A confidential dataset cannot be poured into an object whose fields every
// role reads; once the fields name their readers the pipeline runs and the
// output dataset inherits the marking. Restricted data never leaves as CSV.
