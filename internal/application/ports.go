package application

// TransactionManager is intentionally kept out of the application layer for now.
// The PostgreSQL adapter owns the concrete transaction callback because the
// application does not yet have a database use case to coordinate.
