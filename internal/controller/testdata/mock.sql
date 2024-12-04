-- Create a table with a primary key and a sequence
CREATE TABLE users (
    user_id SERIAL PRIMARY KEY,
    username VARCHAR(50) UNIQUE NOT NULL,
    email VARCHAR(100) UNIQUE NOT NULL
);

-- Create another table with a foreign key reference to users
CREATE TABLE orders (
    order_id SERIAL PRIMARY KEY,
    user_id INT REFERENCES users(user_id) ON DELETE CASCADE,
    order_total NUMERIC(10, 2) NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Create a table with a check constraint
CREATE TABLE products (
    product_id SERIAL PRIMARY KEY,
    name VARCHAR(100) NOT NULL,
    price NUMERIC(10, 2) NOT NULL CHECK (price > 0)
);

-- Create a function to calculate the total price of orders for a user
CREATE OR REPLACE FUNCTION calculate_user_total(userId INT) RETURNS NUMERIC AS $$
    DECLARE
        total NUMERIC(10, 2);
    BEGIN
        SELECT COALESCE(SUM(order_total), 0) INTO total
        FROM orders WHERE user_id = userId;
        RETURN total;
    END;
$$ LANGUAGE plpgsql;

-- Create an index on the orders table to optimize queries by created_at
CREATE INDEX idx_orders_created_at ON orders(created_at);

-- Create a materialized view that summarizes total orders per user
CREATE MATERIALIZED VIEW user_order_totals AS
SELECT u.user_id, u.username, COALESCE(SUM(o.order_total), 0) AS total_spent
FROM users u
LEFT JOIN orders o ON u.user_id = o.user_id
GROUP BY u.user_id, u.username;

-- Create a trigger to log inserts into the orders table
CREATE TABLE order_logs (
    log_id SERIAL PRIMARY KEY,
    order_id INT NOT NULL,
    log_time TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE OR REPLACE FUNCTION log_order_insert() RETURNS TRIGGER AS $$
BEGIN
    INSERT INTO order_logs (order_id) VALUES (NEW.order_id);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER after_order_insert
AFTER INSERT ON orders
FOR EACH ROW EXECUTE FUNCTION log_order_insert();

-- Create a table with an exclusion constraint
CREATE TABLE event_schedule (
    event_id SERIAL PRIMARY KEY,
    event_name VARCHAR(100) NOT NULL,
    event_time TSRANGE NOT NULL,
    EXCLUDE USING gist (event_time WITH &&)  -- Ensures no overlapping events
);
