package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/smtp"
	"os"
	"strconv"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

var (
	queueHost           = os.Getenv("QUEUE_HOST")
	queueUser           = os.Getenv("QUEUE_USER")
	queuePass           = os.Getenv("QUEUE_PASS")
	smtpHost            = os.Getenv("SMTP_HOST")
	smtpPort            = os.Getenv("SMTP_PORT")
	smtpUsername        = os.Getenv("SMTP_USERNAME")
	smtpPassword        = os.Getenv("SMTP_PASSWORD")
	targetEmail         = os.Getenv("TARGET_EMAIL")
	concurrencyLimit, _ = strconv.Atoi(os.Getenv("CONCURRENCY_LIMIT"))
	queueName           = "event_queue"
	amqpConn            *amqp.Connection
	amqpChannel         *amqp.Channel
	workerPool          chan struct{}
)

type VehicleEvent struct {
	Type         string `json:"type"`
	VehiclePlate string `json:"vehicle_plate"`
	Status       string `json:"status"`
}

func main() {
	connectToQueue()

	defer func() {
		if amqpChannel != nil {
			amqpChannel.Close()
		}
		if amqpConn != nil {
			amqpConn.Close()
		}
	}()

	workerPool = make(chan struct{}, concurrencyLimit)
	go startWorker()

	http.HandleFunc("/api/event", eventHandler)

	log.Printf("Servidor escuchando en :8080. Cantidad de Workers: %d", concurrencyLimit)
	log.Fatal(http.ListenAndServe(":8080", nil))
}

// Aquí esta la cola paralela de rabbit mq
func connectToQueue() {
	url := fmt.Sprintf("amqp://%s:%s@%s:5672/", queueUser, queuePass, queueHost)
	var err error
	for range 5 {
		amqpConn, err = amqp.Dial(url)
		if err == nil {
			amqpChannel, err = amqpConn.Channel()
			if err == nil {
				_, err = amqpChannel.QueueDeclare(
					queueName, // name
					true,      // durable (persistente)
					false,     // delete when unused
					false,     // exclusive
					false,     // no-wait
					nil,       // arguments
				)
				if err == nil {
					log.Println("Conexión y Cola RabbitMQ establecida correctamente.")
					return
				}
			}
		}
		log.Printf("[Error]: No se pudo conectar a RabbitMQ, reinentando...:  %v", err)
		time.Sleep(5 * time.Second)
	}
	log.Fatalf("[Error]: No se pudo conectar a RabbitMQ.")
}

// Aquí llega la request por primera vez, se maneja el envio a la cola y luego
func eventHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "[Error]: Solicitud HTTP no es método POST.", http.StatusMethodNotAllowed)
		return
	}

	var event VehicleEvent
	if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
		http.Error(w, "[Error]: Payload JSON del la request inválido.", http.StatusBadRequest)
		return
	}

	body, _ := json.Marshal(event)

	err := amqpChannel.PublishWithContext(
		context.Background(),
		"",        // exchange
		queueName, // routing key
		false,     // mandatory
		false,     // immediate
		amqp.Publishing{
			ContentType: "application/json",
			Body:        body,
		})

	if err != nil {
		log.Printf("[Error]: Al encolar mensaje a RabbitMQ: %v", err)
		http.Error(w, "[Error]: Al encolar mensaje a RabbitMQ.", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "Evento recibido y encolado.")
}

// La logica del trabajador, donde revisa si es una emergencia y la redirecciona a la función.
func startWorker() {
	msgs, err := amqpChannel.ConsumeWithContext(
		context.Background(),
		queueName, // queue
		"",        // consumer
		true,      // auto-ack -> auto elimina al leer el mensaje
		false,     // exclusive
		false,     // no-local
		false,     // no-wait
		nil,       // args
	)
	if err != nil {
		log.Fatalf("[Error]: Al consumir el mensaje de la cola de RabbitMQ (Worker): %v", err)
	}

	for d := range msgs {
		workerPool <- struct{}{}

		go func(delivery amqp.Delivery) {
			defer func() {
				<-workerPool
			}()

			var event VehicleEvent
			if err := json.Unmarshal(delivery.Body, &event); err != nil {
				log.Printf("[Error]: Mensaje JSON inválido (Worker): %v", err)
				return
			}

			if event.Type == "Emergency" {
				log.Printf("[LOG]: Recibido evento de emergencia para %s a las: %s", event.VehiclePlate, time.Now().Format(time.RFC3339Nano))
				sendEmailAlert(event)
			}
		}(d)
	}
}

func sendEmailAlert(event VehicleEvent) {
	to := []string{targetEmail}
	msg := fmt.Appendf(nil,
		"To: %s\r\n"+
			"Subject: Alerta de Emergencia - Vehículo %s\r\n"+
			"\r\n"+
			"Se ha detectado un evento de emergencia de tipo: %s para el vehículo con placa: %s. con Estado: %s",
		targetEmail,
		event.VehiclePlate,
		event.Type,
		event.VehiclePlate,
		event.Status,
	)

	addr := fmt.Sprintf("%s:%s", smtpHost, smtpPort)
	var auth smtp.Auth
	if smtpUsername != "" {
		auth = smtp.PlainAuth("", smtpUsername, smtpPassword, smtpHost)
	}

	err := smtp.SendMail(addr, auth, "alerta@sistema.com", to, msg)
	if err != nil {
		log.Printf("[Error]: Al enviar el correo electrónico (SMTP) para %s: %v", event.VehiclePlate, err)
		return
	}

	log.Printf("[LOG] Correo enviado exitosamente para %s a las: %s", event.VehiclePlate, time.Now().Format(time.RFC3339Nano))
}
